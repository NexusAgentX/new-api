package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoveryEnableRejectsAnOlderStatusEvent(t *testing.T) {
	truncateTables(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &Channel{
		Name:   "failure-sample-stale-recovery-test",
		Key:    "test-key",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(channel).Error)
	first, err := UpdateChannelStatusWithEventDetailed(channel.Id, "test-key", common.ChannelStatusAutoDisabled, "first failure", ChannelStatusChange{
		Source: "relay_error", ReasonCode: "first_failure", ReasonDetail: "first failure",
	})
	require.NoError(t, err)
	require.True(t, first.OverallChanged)
	require.True(t, EnableChannelIfAutoDisabledWithEvent(channel.Id, "test-key", ChannelStatusChange{
		Source: "manual", ReasonCode: "manual_enable", ReasonDetail: "manual enable",
	}))
	second, err := UpdateChannelStatusWithEventDetailed(channel.Id, "test-key", common.ChannelStatusAutoDisabled, "second failure", ChannelStatusChange{
		Source: "relay_error", ReasonCode: "second_failure", ReasonDetail: "second failure",
	})
	require.NoError(t, err)
	require.True(t, second.OverallChanged)

	assert.False(t, EnableChannelIfAutoDisabledWithEvent(channel.Id, "test-key", ChannelStatusChange{
		Source: "recovery", ReasonCode: "recovery_succeeded", ReasonDetail: "old probe succeeded",
		ExpectedStatusEventId: common.GetPointer(first.StatusEventId),
	}))
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
}

func TestEnableChannelIfAutoDisabledRequiresCurrentAutoDisabledStatus(t *testing.T) {
	truncateTables(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &Channel{
		Name:   "recovery-test",
		Key:    "test-key",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusManuallyDisabled,
	}
	require.NoError(t, DB.Create(channel).Error)

	assert.False(t, EnableChannelIfAutoDisabled(channel.Id, ""))
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)

	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"status":     common.ChannelStatusAutoDisabled,
		"other_info": `{"status_reason":"timeout","status_reason_code":"upstream_timeout"}`,
	}).Error)

	assert.True(t, EnableChannelIfAutoDisabledWithEvent(channel.Id, "", ChannelStatusChange{
		Source:       "channel_test",
		ReasonCode:   "recovery_succeeded",
		ReasonDetail: "Recovery probe succeeded",
		RequestId:    "req-recovery",
	}))
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Empty(t, stored.GetOtherInfo()["status_reason"])
	assert.False(t, EnableChannelIfAutoDisabled(channel.Id, ""))

	var events []ChannelStatusEvent
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Order("id").Find(&events).Error)
	require.Len(t, events, 1)
	assert.Equal(t, "channel", events[0].Scope)
	assert.Equal(t, common.ChannelStatusAutoDisabled, events[0].FromStatus)
	assert.Equal(t, common.ChannelStatusEnabled, events[0].ToStatus)
	assert.Equal(t, "upstream_timeout", events[0].PreviousReasonCode)
	assert.Equal(t, "timeout", events[0].PreviousReasonDetail)
	assert.Equal(t, "req-recovery", events[0].RequestId)
}

func TestUpdateChannelStatusWithEventTracksMultiKeyWithoutPersistingKeys(t *testing.T) {
	truncateTables(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &Channel{
		Name:   "multi-key-audit-test",
		Key:    "secret-key-one\nsecret-key-two",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	assert.True(t, UpdateChannelStatusWithEvent(channel.Id, "secret-key-one", common.ChannelStatusAutoDisabled, "Bearer leaked-token secret-key-one", ChannelStatusChange{
		Source:       "relay_error",
		ReasonCode:   "upstream_http_429",
		ReasonDetail: "Bearer leaked-token secret-key-one",
	}))
	assert.True(t, UpdateChannelStatusWithEvent(channel.Id, "secret-key-two", common.ChannelStatusAutoDisabled, "second key failed", ChannelStatusChange{
		Source:       "relay_error",
		ReasonCode:   "upstream_http_500",
		ReasonDetail: "second key failed",
	}))
	assert.True(t, EnableChannelIfAutoDisabledWithEvent(channel.Id, "secret-key-one", ChannelStatusChange{
		Source:       "scheduled_channel_test",
		ReasonCode:   "channel_test_succeeded",
		ReasonDetail: "Recovery succeeded",
	}))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 0)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])

	var events []ChannelStatusEvent
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Order("id").Find(&events).Error)
	require.Len(t, events, 5)
	for _, event := range events {
		assert.NotContains(t, event.ReasonDetail, "secret-key")
		assert.NotContains(t, event.ReasonDetail, "leaked-token")
	}
	for _, index := range []int{0, 1, 3} {
		assert.Equal(t, "key", events[index].Scope)
		require.NotNil(t, events[index].KeyIndex)
		assert.Len(t, events[index].KeyFingerprint, 16)
	}
	assert.Equal(t, 0, *events[0].KeyIndex)
	assert.Equal(t, ChannelKeyFingerprint("secret-key-one"), events[0].KeyFingerprint)
	assert.Equal(t, common.ChannelStatusEnabled, events[0].ChannelStatusAfter)
	assert.Equal(t, common.ChannelStatusAutoDisabled, events[1].ChannelStatusAfter)
	assert.Equal(t, "channel", events[2].Scope)
	assert.Equal(t, "all_keys_disabled", events[2].ReasonCode)
	assert.Equal(t, "All keys are disabled", events[2].ReasonDetail)
	assert.Equal(t, "upstream_http_429", events[3].PreviousReasonCode)
	assert.NotContains(t, events[3].PreviousReasonDetail, "secret-key-one")
	assert.NotContains(t, events[3].PreviousReasonDetail, "leaked-token")
	assert.Equal(t, "channel", events[4].Scope)
	assert.Equal(t, "channel_test_succeeded", events[4].ReasonCode)
	assert.Equal(t, "all_keys_disabled", events[4].PreviousReasonCode)
	assert.Equal(t, "All keys are disabled", events[4].PreviousReasonDetail)
}

func TestMultiKeyTransitionsDoNotOverrideManualChannelStatus(t *testing.T) {
	truncateTables(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &Channel{
		Name:   "manual-channel-key-transition-test",
		Key:    "secret-key-one\nsecret-key-two",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusManuallyDisabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:                 true,
			MultiKeySize:               2,
			MultiKeyStatusList:         map[int]int{0: common.ChannelStatusAutoDisabled},
			MultiKeyDisabledReason:     map[int]string{0: "automatic"},
			MultiKeyDisabledReasonCode: map[int]string{0: "upstream_http_500"},
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	assert.False(t, EnableChannelIfAutoDisabledWithEvent(channel.Id, "secret-key-one", ChannelStatusChange{
		Source:       "passive_recovery_test",
		ReasonCode:   "passive_recovery_succeeded",
		ReasonDetail: "Passive recovery succeeded",
	}))
	assert.True(t, UpdateChannelStatusWithEvent(channel.Id, "secret-key-one", common.ChannelStatusEnabled, "Key manually enabled", ChannelStatusChange{
		Source:       "manual",
		ReasonCode:   "manual_enable",
		ReasonDetail: "Key manually enabled",
	}))
	assert.True(t, UpdateChannelStatusWithEvent(channel.Id, "secret-key-two", common.ChannelStatusManuallyDisabled, "Key manually disabled", ChannelStatusChange{
		Source:       "manual",
		ReasonCode:   "manual_disable",
		ReasonDetail: "Key manually disabled",
	}))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 0)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.ChannelInfo.MultiKeyStatusList[1])

	var events []ChannelStatusEvent
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Order("id").Find(&events).Error)
	require.Len(t, events, 2)
	assert.Equal(t, "key", events[0].Scope)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, events[0].ChannelStatusAfter)
	assert.Equal(t, "key", events[1].Scope)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, events[1].ChannelStatusAfter)
}

func TestUpdateChannelStatusWithEventPreservesWholeMultiKeyDisableReason(t *testing.T) {
	truncateTables(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &Channel{
		Name:      "multi-key-channel-recovery-test",
		Key:       "secret-key-one\nsecret-key-two",
		Models:    "gpt-test",
		Group:     "default",
		Status:    common.ChannelStatusAutoDisabled,
		OtherInfo: `{"status_reason":"All keys are disabled","status_reason_code":"all_keys_disabled"}`,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	assert.True(t, UpdateChannelStatusWithEvent(channel.Id, "", common.ChannelStatusEnabled, "Manually recovered", ChannelStatusChange{
		Source:       "manual",
		ReasonCode:   "manual_enable",
		ReasonDetail: "Manually recovered",
	}))

	var event ChannelStatusEvent
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&event).Error)
	assert.Equal(t, "all_keys_disabled", event.PreviousReasonCode)
	assert.Equal(t, "All keys are disabled", event.PreviousReasonDetail)
}

func TestChannelUpdateRemovesOutOfRangeMultiKeyStatusMetadata(t *testing.T) {
	truncateTables(t)
	channel := &Channel{
		Name:   "multi-key-status-metadata-cleanup",
		Key:    "key-zero\nkey-one\nkey-two",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:                 true,
			MultiKeySize:               3,
			MultiKeyStatusList:         map[int]int{0: common.ChannelStatusManuallyDisabled, 2: common.ChannelStatusAutoDisabled},
			MultiKeyDisabledTime:       map[int]int64{0: 100, 2: 200},
			MultiKeyDisabledReason:     map[int]string{0: "manual", 2: "automatic"},
			MultiKeyDisabledReasonCode: map[int]string{0: "manual_disable", 2: "upstream_http_500"},
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	channel.Key = "key-zero"
	require.NoError(t, channel.Update())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeySize)
	assert.Equal(t, map[int]int{0: common.ChannelStatusManuallyDisabled}, stored.ChannelInfo.MultiKeyStatusList)
	assert.Equal(t, map[int]int64{0: 100}, stored.ChannelInfo.MultiKeyDisabledTime)
	assert.Equal(t, map[int]string{0: "manual"}, stored.ChannelInfo.MultiKeyDisabledReason)
	assert.Equal(t, map[int]string{0: "manual_disable"}, stored.ChannelInfo.MultiKeyDisabledReasonCode)
}

func TestUpdateChannelStatusWithEventRollsBackStatusWhenAuditInsertFails(t *testing.T) {
	truncateTables(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &Channel{
		Name:   "status-event-rollback-test",
		Key:    "test-key",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Migrator().DropTable(&ChannelStatusEvent{}))
	t.Cleanup(func() {
		require.NoError(t, DB.AutoMigrate(&ChannelStatusEvent{}))
	})

	changed, err := UpdateChannelStatusWithEventResult(channel.Id, "", common.ChannelStatusAutoDisabled, "upstream failed", ChannelStatusChange{
		Source:       "relay_error",
		ReasonCode:   "upstream_http_500",
		ReasonDetail: "upstream failed",
	})
	require.Error(t, err)
	assert.False(t, changed)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.False(t, strings.Contains(stored.OtherInfo, "upstream failed"))
}

func TestUpdateChannelStatusByTagReturnsAuditFailure(t *testing.T) {
	truncateTables(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	tag := "status-event-tag"
	channel := &Channel{
		Name:   "tag-status-event-rollback-test",
		Key:    "test-key",
		Models: "gpt-test",
		Group:  "default",
		Tag:    &tag,
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Migrator().DropTable(&ChannelStatusEvent{}))
	t.Cleanup(func() {
		require.NoError(t, DB.AutoMigrate(&ChannelStatusEvent{}))
	})

	err := UpdateChannelStatusByTagWithEvent("status-event-tag", common.ChannelStatusManuallyDisabled, ChannelStatusChange{
		Source:       "manual_tag",
		ReasonCode:   "manual_tag_disable",
		ReasonDetail: "Channel disabled by tag",
	})
	require.Error(t, err)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
}
