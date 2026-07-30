package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnableChannelNotifiesOnceAfterConditionalRecovery(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
	require.NoError(t, model.DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM users").Error)
	notifyLimitStore.Range(func(key, value any) bool {
		notifyLimitStore.Delete(key)
		return true
	})

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		notifyLimitStore.Range(func(key, value any) bool {
			notifyLimitStore.Delete(key)
			return true
		})
	})

	root := &model.User{
		Username: "root-recovery-test",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, model.DB.Create(root).Error)
	channel := &model.Channel{
		Name:   "recovery-notification-test",
		Key:    "test-key",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusAutoDisabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)

	assert.True(t, EnableChannel(channel.Id, "", channel.Name))
	assert.False(t, EnableChannel(channel.Id, "", channel.Name))

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)

	notifyType := formatNotifyType(channel.Id, common.ChannelStatusEnabled)
	notifyKey := fmt.Sprintf("%d:%s:%s", root.Id, notifyType, time.Now().Format("2006010215"))
	value, exists := notifyLimitStore.Load(notifyKey)
	require.True(t, exists)
	count, ok := value.(limitCount)
	require.True(t, ok)
	assert.Equal(t, 1, count.Count)

	seenMatchingType := 0
	notifyLimitStore.Range(func(key, value any) bool {
		if strings.Contains(fmt.Sprint(key), ":"+dto.NotifyTypeChannelUpdate+"_") {
			seenMatchingType++
		}
		return true
	})
	assert.Equal(t, 1, seenMatchingType)
}
