package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveChannelFailureSampleIfCurrentRejectsLateOlderTransition(t *testing.T) {
	truncateTables(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &Channel{
		Name:   "late-failure-sample-test",
		Key:    "test-key",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(channel).Error)
	first, err := UpdateChannelStatusWithEventDetailed(channel.Id, channel.Key, common.ChannelStatusAutoDisabled, "first failure", ChannelStatusChange{
		Source: "first_response_policy", ReasonCode: "first_timeout", ReasonDetail: "first failure",
	})
	require.NoError(t, err)
	require.True(t, first.OverallChanged)
	require.True(t, EnableChannelIfAutoDisabled(channel.Id, channel.Key))
	second, err := UpdateChannelStatusWithEventDetailed(channel.Id, channel.Key, common.ChannelStatusAutoDisabled, "second failure", ChannelStatusChange{
		Source: "first_response_policy", ReasonCode: "second_timeout", ReasonDetail: "second failure",
	})
	require.NoError(t, err)
	require.True(t, second.OverallChanged)

	newBody := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"new"}]}`)
	stored, err := SaveChannelFailureSampleIfCurrent(&ChannelFailureSample{
		ChannelId: channel.Id, DisableEventId: second.StatusEventId,
		RequestBody: newBody, RequestPath: "/v1/chat/completions", RelayFormat: "openai", Model: "gpt-test",
		CapturedAt: time.Now().Unix(), BodySize: int64(len(newBody)),
	})
	require.NoError(t, err)
	require.True(t, stored)

	oldBody := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"old"}]}`)
	stored, err = SaveChannelFailureSampleIfCurrent(&ChannelFailureSample{
		ChannelId: channel.Id, DisableEventId: first.StatusEventId,
		RequestBody: oldBody, RequestPath: "/v1/chat/completions", RelayFormat: "openai", Model: "gpt-test",
		CapturedAt: time.Now().Unix(), BodySize: int64(len(oldBody)),
	})
	require.NoError(t, err)
	assert.False(t, stored)

	sample, err := GetChannelFailureSample(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, second.StatusEventId, sample.DisableEventId)
	assert.Equal(t, newBody, sample.RequestBody)
}

func TestDeleteExpiredChannelFailureSamplesKeepsRetentionBoundary(t *testing.T) {
	truncateTables(t)
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"model":"gpt-test"}`)
	for _, sample := range []*ChannelFailureSample{
		{
			ChannelId:   1001,
			RequestBody: body,
			RequestPath: "/v1/chat/completions",
			RelayFormat: "openai",
			Model:       "gpt-test",
			CapturedAt:  now.Add(-ChannelFailureSampleTTL - time.Second).Unix(),
			BodySize:    int64(len(body)),
		},
		{
			ChannelId:   1002,
			RequestBody: body,
			RequestPath: "/v1/chat/completions",
			RelayFormat: "openai",
			Model:       "gpt-test",
			CapturedAt:  now.Add(-ChannelFailureSampleTTL).Unix(),
			BodySize:    int64(len(body)),
		},
	} {
		require.NoError(t, saveChannelFailureSample(DB, sample))
	}

	require.NoError(t, DeleteExpiredChannelFailureSamples(now))
	var channelIds []int
	require.NoError(t, DB.Model(&ChannelFailureSample{}).Order("channel_id").Pluck("channel_id", &channelIds).Error)
	assert.Equal(t, []int{1002}, channelIds)
}
