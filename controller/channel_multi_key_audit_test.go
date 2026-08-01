package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func performMultiKeyAction(t *testing.T, request MultiKeyManageRequest, actorUserId int, requestId string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := common.Marshal(request)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", actorUserId)
	ctx.Set("role", common.RoleRootUser)
	ctx.Set(common.RequestIdKey, requestId)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/multi_key/manage", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	ManageMultiKeys(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	return recorder
}

func TestManageMultiKeysAuditsManualKeyDisableAndRecovery(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &model.Channel{
		Name:   "manual-multi-key-audit",
		Key:    "secret-key-one\nsecret-key-two",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, db.Create(channel).Error)

	keyIndex := 0
	performMultiKeyAction(t, MultiKeyManageRequest{
		ChannelId: channel.Id,
		Action:    "disable_key",
		KeyIndex:  &keyIndex,
	}, 42, "req-manual-key-disable")

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "manual_disable", stored.ChannelInfo.MultiKeyDisabledReasonCode[0])

	performMultiKeyAction(t, MultiKeyManageRequest{
		ChannelId: channel.Id,
		Action:    "enable_key",
		KeyIndex:  &keyIndex,
	}, 42, "req-manual-key-enable")

	var events []model.ChannelStatusEvent
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Order("id").Find(&events).Error)
	require.Len(t, events, 2)
	assert.Equal(t, "key", events[0].Scope)
	assert.Equal(t, "manual", events[0].Source)
	assert.Equal(t, "manual_disable", events[0].ReasonCode)
	assert.Equal(t, 42, events[0].ActorUserId)
	assert.Equal(t, "req-manual-key-disable", events[0].RequestId)
	assert.Equal(t, model.ChannelKeyFingerprint("secret-key-one"), events[0].KeyFingerprint)
	assert.NotContains(t, events[0].ReasonDetail, "secret-key-one")
	assert.Equal(t, "manual_enable", events[1].ReasonCode)
	assert.Equal(t, "manual_disable", events[1].PreviousReasonCode)
	assert.Equal(t, "req-manual-key-enable", events[1].RequestId)
}

func TestManageMultiKeysAuditsWholeChannelBulkTransitions(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &model.Channel{
		Name:   "bulk-multi-key-audit",
		Key:    "bulk-secret-one\nbulk-secret-two",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, db.Create(channel).Error)

	performMultiKeyAction(t, MultiKeyManageRequest{
		ChannelId: channel.Id,
		Action:    "disable_all_keys",
	}, 43, "req-manual-key-disable-all")

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	assert.Equal(t, "manual_disable", stored.ChannelInfo.MultiKeyDisabledReasonCode[0])
	assert.Equal(t, "manual_disable", stored.ChannelInfo.MultiKeyDisabledReasonCode[1])

	performMultiKeyAction(t, MultiKeyManageRequest{
		ChannelId: channel.Id,
		Action:    "enable_all_keys",
	}, 43, "req-manual-key-enable-all")

	var events []model.ChannelStatusEvent
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Order("id").Find(&events).Error)
	require.Len(t, events, 6)
	keyEvents := 0
	channelEvents := 0
	for _, event := range events {
		assert.Equal(t, "manual_batch", event.Source)
		assert.Equal(t, 43, event.ActorUserId)
		if event.Scope == "key" {
			keyEvents++
		} else if event.Scope == "channel" {
			channelEvents++
		}
	}
	assert.Equal(t, 4, keyEvents)
	assert.Equal(t, 2, channelEvents)
	assert.Equal(t, "all_keys_disabled", events[2].ReasonCode)
	assert.Equal(t, "all_keys_disabled", events[4].PreviousReasonCode)

	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Empty(t, stored.ChannelInfo.MultiKeyStatusList)
	assert.Empty(t, stored.ChannelInfo.MultiKeyDisabledReasonCode)
}

func TestManageMultiKeysReindexesDisabledReasonCodesAfterDeletion(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &model.Channel{
		Name:   "delete-multi-key-reason-code",
		Key:    "delete-key-zero\ndelete-key-one\ndelete-key-two",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:                 true,
			MultiKeySize:               3,
			MultiKeyStatusList:         map[int]int{1: common.ChannelStatusAutoDisabled, 2: common.ChannelStatusManuallyDisabled},
			MultiKeyDisabledReason:     map[int]string{1: "automatic", 2: "manual"},
			MultiKeyDisabledReasonCode: map[int]string{1: "upstream_http_500", 2: "manual_disable"},
		},
	}
	require.NoError(t, db.Create(channel).Error)

	performMultiKeyAction(t, MultiKeyManageRequest{
		ChannelId: channel.Id,
		Action:    "delete_disabled_keys",
	}, 44, "req-delete-disabled-keys")

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, []string{"delete-key-zero", "delete-key-two"}, stored.GetKeys())
	assert.Equal(t, "manual_disable", stored.ChannelInfo.MultiKeyDisabledReasonCode[1])

	keyIndex := 0
	performMultiKeyAction(t, MultiKeyManageRequest{
		ChannelId: channel.Id,
		Action:    "delete_key",
		KeyIndex:  &keyIndex,
	}, 44, "req-delete-key")

	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, []string{"delete-key-two"}, stored.GetKeys())
	assert.Equal(t, "manual_disable", stored.ChannelInfo.MultiKeyDisabledReasonCode[0])
}
