package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstResponseTimeoutUsesDedicatedDisableRule(t *testing.T) {
	originalEnabled := common.AutomaticDisableChannelEnabled
	originalRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = originalEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = originalRanges
	})

	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 524, End: 524}}

	err := types.NewUpstreamFirstResponseTimeoutError(20)
	require.Equal(t, 524, err.StatusCode)
	assert.False(t, ShouldDisableChannel(err))
}

func resetFirstResponseTimeoutMemory(t *testing.T) {
	t.Helper()
	firstResponseTimeoutMemory.Lock()
	originalBuckets := firstResponseTimeoutMemory.Buckets
	firstResponseTimeoutMemory.Buckets = make(map[int]map[int64]firstResponseTimeoutCounts)
	firstResponseTimeoutMemory.Unlock()
	t.Cleanup(func() {
		firstResponseTimeoutMemory.Lock()
		firstResponseTimeoutMemory.Buckets = originalBuckets
		firstResponseTimeoutMemory.Unlock()
	})
}

func TestFirstResponseTimeoutRollingWindowCountsAttempts(t *testing.T) {
	resetFirstResponseTimeoutMemory(t)

	now := time.Unix(1_800_000_000, 0)
	recordFirstResponseAttemptMemory(7, false, 2, now)
	recordFirstResponseAttemptMemory(7, false, 2, now)
	counts := recordFirstResponseAttemptMemory(7, true, 2, now)

	assert.Equal(t, int64(3), counts.Total)
	assert.Equal(t, int64(1), counts.Timeouts)

	counts = recordFirstResponseAttemptMemory(7, true, 2, now.Add(2*time.Minute))
	assert.Equal(t, int64(1), counts.Total)
	assert.Equal(t, int64(1), counts.Timeouts)
}

func TestReachedFirstResponseTimeoutDisableThreshold(t *testing.T) {
	setting := &operation_setting.FirstResponseTimeoutSetting{
		DisableRate:               30,
		DisableMinTimeoutAttempts: 2,
	}

	tests := []struct {
		name   string
		counts firstResponseTimeoutCounts
		want   bool
	}{
		{name: "single timeout does not meet minimum", counts: firstResponseTimeoutCounts{Total: 1, Timeouts: 1}, want: false},
		{name: "two timeouts meet rate and minimum", counts: firstResponseTimeoutCounts{Total: 2, Timeouts: 2}, want: true},
		{name: "two timeouts below rate", counts: firstResponseTimeoutCounts{Total: 7, Timeouts: 2}, want: false},
		{name: "three of ten meets rate", counts: firstResponseTimeoutCounts{Total: 10, Timeouts: 3}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, reachedFirstResponseTimeoutDisableThreshold(test.counts, setting))
		})
	}
}

func TestGetFirstResponseTimeoutCountsMemoryReturnsPerChannelWindow(t *testing.T) {
	resetFirstResponseTimeoutMemory(t)

	now := time.Unix(1_800_000_000, 0)
	recordFirstResponseAttemptMemory(7, false, 2, now)
	recordFirstResponseAttemptMemory(7, true, 2, now)
	recordFirstResponseAttemptMemory(8, true, 2, now.Add(-time.Minute))
	recordFirstResponseAttemptMemory(8, true, 2, now.Add(-2*time.Minute))

	counts := getFirstResponseTimeoutCountsMemory([]int{7, 8, 9}, 2, now)

	assert.Equal(t, firstResponseTimeoutCounts{Total: 2, Timeouts: 1}, counts[7])
	assert.Equal(t, firstResponseTimeoutCounts{Total: 1, Timeouts: 1}, counts[8])
	assert.Equal(t, firstResponseTimeoutCounts{}, counts[9])
}

func TestGetFirstResponseTimeoutStatsReturnsRateForEveryChannel(t *testing.T) {
	resetFirstResponseTimeoutMemory(t)
	originalRedisEnabled := common.RedisEnabled
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	originalSetting := *setting
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		*setting = originalSetting
	})

	common.RedisEnabled = false
	setting.DisableWindowMinutes = 2
	now := time.Now()
	recordFirstResponseAttemptMemory(7, false, setting.DisableWindowMinutes, now)
	recordFirstResponseAttemptMemory(7, true, setting.DisableWindowMinutes, now)

	stats := GetFirstResponseTimeoutStats([]int{7, 8, 7})

	require.Contains(t, stats, 7)
	assert.Equal(t, int64(2), stats[7].TotalAttempts)
	assert.Equal(t, int64(1), stats[7].TimeoutAttempts)
	assert.InDelta(t, 50, stats[7].TimeoutRate, 0.001)
	assert.Equal(t, 2, stats[7].WindowMinutes)
	require.Contains(t, stats, 8)
	assert.Zero(t, stats[8].TotalAttempts)
	assert.Zero(t, stats[8].TimeoutRate)
}
