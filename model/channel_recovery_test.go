package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		"other_info": `{"status_reason":"timeout"}`,
	}).Error)

	assert.True(t, EnableChannelIfAutoDisabled(channel.Id, ""))
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Empty(t, stored.GetOtherInfo()["status_reason"])
	assert.False(t, EnableChannelIfAutoDisabled(channel.Id, ""))
}
