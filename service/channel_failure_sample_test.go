package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFailureSamplePersistenceErrorDoesNotBlockAutoDisable(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &model.Channel{
		Name:   "failure-sample-persistence-error-test",
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	t.Cleanup(func() {
		model.DB.Where("channel_id = ?", channel.Id).Delete(&model.ChannelFailureSample{})
		model.DB.Where("channel_id = ?", channel.Id).Delete(&model.ChannelStatusEvent{})
		model.DB.Delete(&model.Channel{}, channel.Id)
	})

	callbackName := "test:fail_channel_failure_sample_create"
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "channel_failure_samples" {
			tx.AddError(errors.New("forced failure sample persistence error"))
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Callback().Create().Remove(callbackName))
	})

	channelError := *types.NewChannelError(channel.Id, channel.Type, channel.Name, false, channel.Key, true)
	body := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`)
	disableChannelWithFailureSample(channelError, model.ChannelStatusChange{
		Source:       "first_response_policy",
		ReasonCode:   "first_response_timeout_rate",
		ReasonDetail: "timeout threshold reached",
		RequestId:    "request-persistence-error",
	}, &model.ChannelFailureSample{
		RequestBody: body,
		RequestPath: "/v1/chat/completions",
		RelayFormat: "openai",
		Model:       "gpt-test",
		RequestId:   "request-persistence-error",
		CapturedAt:  time.Now().Unix(),
		BodySize:    int64(len(body)),
	})

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	_, err := model.GetChannelFailureSample(channel.Id)
	assert.Error(t, err)
}

func TestConcurrentAutoDisableStoresOnlyTheTransitionWinnerAndOverwritesNextCycle(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &model.Channel{
		Name:   "failure-sample-service-test",
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	t.Cleanup(func() {
		model.DB.Where("channel_id = ?", channel.Id).Delete(&model.ChannelFailureSample{})
		model.DB.Where("channel_id = ?", channel.Id).Delete(&model.ChannelStatusEvent{})
		model.DB.Delete(&model.Channel{}, channel.Id)
	})

	channelError := *types.NewChannelError(channel.Id, channel.Type, channel.Name, false, channel.Key, true)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for _, requestId := range []string{"request-a", "request-b"} {
		requestId := requestId
		body := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"` + requestId + `"}]}`)
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			disableChannelWithFailureSample(channelError, model.ChannelStatusChange{
				Source:       "first_response_policy",
				ReasonCode:   "first_response_timeout_rate",
				ReasonDetail: "timeout threshold reached",
				RequestId:    requestId,
			}, &model.ChannelFailureSample{
				RequestBody: body,
				RequestPath: "/v1/chat/completions",
				RelayFormat: "openai",
				Model:       "gpt-test",
				RequestId:   requestId,
				CapturedAt:  time.Now().Unix(),
				BodySize:    int64(len(body)),
			})
		}()
	}
	close(start)
	waitGroup.Wait()

	sample, err := model.GetChannelFailureSample(channel.Id)
	require.NoError(t, err)
	var events []model.ChannelStatusEvent
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Order("id").Find(&events).Error)
	require.Len(t, events, 1)
	assert.Equal(t, events[0].Id, sample.DisableEventId)
	assert.Equal(t, events[0].RequestId, sample.RequestId)
	assert.Contains(t, string(sample.RequestBody), sample.RequestId)

	require.True(t, model.EnableChannelIfAutoDisabled(channel.Id, channel.Key))
	nextBody := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"request-c"}]}`)
	disableChannelWithFailureSample(channelError, model.ChannelStatusChange{
		Source:       "first_response_policy",
		ReasonCode:   "first_response_timeout_rate",
		ReasonDetail: "next timeout threshold reached",
		RequestId:    "request-c",
	}, &model.ChannelFailureSample{
		RequestBody: nextBody,
		RequestPath: "/v1/chat/completions",
		RelayFormat: "openai",
		Model:       "gpt-test",
		RequestId:   "request-c",
		CapturedAt:  time.Now().Unix(),
		BodySize:    int64(len(nextBody)),
	})

	overwritten, err := model.GetChannelFailureSample(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "request-c", overwritten.RequestId)
	assert.Equal(t, nextBody, overwritten.RequestBody)
}
