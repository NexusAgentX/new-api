package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/go-redis/redis/v8"
)

type firstResponseTimeoutCounts struct {
	Total    int64
	Timeouts int64
}

var firstResponseTimeoutMemory = struct {
	sync.Mutex
	Buckets map[int]map[int64]firstResponseTimeoutCounts
}{Buckets: make(map[int]map[int64]firstResponseTimeoutCounts)}

func firstResponseTimeoutBucketKey(channelID int, minute int64) string {
	return fmt.Sprintf("new-api:first-response-timeout:%d:%d", channelID, minute)
}

func addFirstResponseTimeoutBucket(counts *firstResponseTimeoutCounts, values []any) error {
	if len(values) != 2 {
		return nil
	}
	if values[0] != nil {
		value, err := strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
		if err != nil {
			return err
		}
		counts.Total += value
	}
	if values[1] != nil {
		value, err := strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
		if err != nil {
			return err
		}
		counts.Timeouts += value
	}
	return nil
}

func recordFirstResponseAttemptRedis(channelID int, timedOut bool, windowMinutes int, now time.Time) (firstResponseTimeoutCounts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	minute := now.Unix() / 60
	key := firstResponseTimeoutBucketKey(channelID, minute)
	pipe := common.RDB.TxPipeline()
	pipe.HIncrBy(ctx, key, "total", 1)
	if timedOut {
		pipe.HIncrBy(ctx, key, "timeouts", 1)
	}
	pipe.Expire(ctx, key, time.Duration(windowMinutes+2)*time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		return firstResponseTimeoutCounts{}, err
	}
	if !timedOut {
		return firstResponseTimeoutCounts{}, nil
	}

	readPipe := common.RDB.Pipeline()
	commands := make([]*redis.SliceCmd, 0, windowMinutes)
	for offset := 0; offset < windowMinutes; offset++ {
		bucketKey := firstResponseTimeoutBucketKey(channelID, minute-int64(offset))
		commands = append(commands, readPipe.HMGet(ctx, bucketKey, "total", "timeouts"))
	}
	if _, err := readPipe.Exec(ctx); err != nil && err != redis.Nil {
		return firstResponseTimeoutCounts{}, err
	}

	var result firstResponseTimeoutCounts
	for _, command := range commands {
		if err := addFirstResponseTimeoutBucket(&result, command.Val()); err != nil {
			return firstResponseTimeoutCounts{}, err
		}
	}
	return result, nil
}

func recordFirstResponseAttemptMemory(channelID int, timedOut bool, windowMinutes int, now time.Time) firstResponseTimeoutCounts {
	minute := now.Unix() / 60
	firstResponseTimeoutMemory.Lock()
	defer firstResponseTimeoutMemory.Unlock()

	buckets := firstResponseTimeoutMemory.Buckets[channelID]
	if buckets == nil {
		buckets = make(map[int64]firstResponseTimeoutCounts)
		firstResponseTimeoutMemory.Buckets[channelID] = buckets
	}
	current := buckets[minute]
	current.Total++
	if timedOut {
		current.Timeouts++
	}
	buckets[minute] = current

	oldestMinute := minute - int64(windowMinutes) + 1
	var result firstResponseTimeoutCounts
	for bucketMinute, counts := range buckets {
		if bucketMinute < oldestMinute {
			delete(buckets, bucketMinute)
			continue
		}
		if bucketMinute <= minute {
			result.Total += counts.Total
			result.Timeouts += counts.Timeouts
		}
	}
	return result
}

func getFirstResponseTimeoutCountsRedis(channelIDs []int, windowMinutes int, now time.Time) (map[int]firstResponseTimeoutCounts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	minute := now.Unix() / 60
	pipe := common.RDB.Pipeline()
	commands := make(map[int][]*redis.SliceCmd, len(channelIDs))
	for _, channelID := range channelIDs {
		channelCommands := make([]*redis.SliceCmd, 0, windowMinutes)
		for offset := 0; offset < windowMinutes; offset++ {
			key := firstResponseTimeoutBucketKey(channelID, minute-int64(offset))
			channelCommands = append(channelCommands, pipe.HMGet(ctx, key, "total", "timeouts"))
		}
		commands[channelID] = channelCommands
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	results := make(map[int]firstResponseTimeoutCounts, len(channelIDs))
	for channelID, channelCommands := range commands {
		var counts firstResponseTimeoutCounts
		for _, command := range channelCommands {
			if err := addFirstResponseTimeoutBucket(&counts, command.Val()); err != nil {
				return nil, err
			}
		}
		results[channelID] = counts
	}
	return results, nil
}

func getFirstResponseTimeoutCountsMemory(channelIDs []int, windowMinutes int, now time.Time) map[int]firstResponseTimeoutCounts {
	minute := now.Unix() / 60
	oldestMinute := minute - int64(windowMinutes) + 1
	results := make(map[int]firstResponseTimeoutCounts, len(channelIDs))

	firstResponseTimeoutMemory.Lock()
	defer firstResponseTimeoutMemory.Unlock()
	for _, channelID := range channelIDs {
		buckets := firstResponseTimeoutMemory.Buckets[channelID]
		var counts firstResponseTimeoutCounts
		for bucketMinute, bucketCounts := range buckets {
			if bucketMinute < oldestMinute {
				delete(buckets, bucketMinute)
				continue
			}
			if bucketMinute <= minute {
				counts.Total += bucketCounts.Total
				counts.Timeouts += bucketCounts.Timeouts
			}
		}
		results[channelID] = counts
	}
	return results
}

func GetFirstResponseTimeoutStats(channelIDs []int) map[int]*dto.FirstResponseTimeoutStats {
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	windowMinutes := setting.DisableWindowMinutes
	if len(channelIDs) == 0 || operation_setting.ValidateFirstResponseDisableWindowMinutes(windowMinutes) != nil {
		return map[int]*dto.FirstResponseTimeoutStats{}
	}

	uniqueIDs := make([]int, 0, len(channelIDs))
	seen := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}
		uniqueIDs = append(uniqueIDs, channelID)
	}

	if len(uniqueIDs) == 0 {
		return map[int]*dto.FirstResponseTimeoutStats{}
	}

	now := time.Now()
	var countsByChannel map[int]firstResponseTimeoutCounts
	if common.RedisEnabled && common.RDB != nil {
		redisCounts, err := getFirstResponseTimeoutCountsRedis(uniqueIDs, windowMinutes, now)
		if err == nil {
			countsByChannel = redisCounts
		} else {
			common.SysError(fmt.Sprintf("failed to read first response timeout stats from Redis: error=%v", err))
		}
	}
	if countsByChannel == nil {
		countsByChannel = getFirstResponseTimeoutCountsMemory(uniqueIDs, windowMinutes, now)
	}

	result := make(map[int]*dto.FirstResponseTimeoutStats, len(uniqueIDs))
	for _, channelID := range uniqueIDs {
		counts := countsByChannel[channelID]
		rate := float64(0)
		if counts.Total > 0 {
			rate = float64(counts.Timeouts) * 100 / float64(counts.Total)
		}
		result[channelID] = &dto.FirstResponseTimeoutStats{
			TotalAttempts:   counts.Total,
			TimeoutAttempts: counts.Timeouts,
			TimeoutRate:     rate,
			WindowMinutes:   windowMinutes,
		}
	}
	return result
}

func recordFirstResponseAttempt(channelID int, timedOut bool, windowMinutes int, now time.Time) firstResponseTimeoutCounts {
	if common.RedisEnabled && common.RDB != nil {
		counts, err := recordFirstResponseAttemptRedis(channelID, timedOut, windowMinutes, now)
		if err == nil {
			return counts
		}
		common.SysError(fmt.Sprintf("failed to record first response timeout stats in Redis: channel_id=%d, error=%v", channelID, err))
	}
	return recordFirstResponseAttemptMemory(channelID, timedOut, windowMinutes, now)
}

func reachedFirstResponseTimeoutDisableThreshold(counts firstResponseTimeoutCounts, setting *operation_setting.FirstResponseTimeoutSetting) bool {
	if counts.Total <= 0 || counts.Timeouts < int64(setting.DisableMinTimeoutAttempts) {
		return false
	}
	rate := float64(counts.Timeouts) * 100 / float64(counts.Total)
	return rate >= setting.DisableRate
}

func RecordFirstResponseAttempt(channelError types.ChannelError, timedOut bool, requestId string) {
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	if channelError.ChannelId <= 0 ||
		operation_setting.ValidateFirstResponseDisableWindowMinutes(setting.DisableWindowMinutes) != nil ||
		operation_setting.ValidateFirstResponseDisableMinTimeoutAttempts(setting.DisableMinTimeoutAttempts) != nil {
		return
	}

	counts := recordFirstResponseAttempt(channelError.ChannelId, timedOut, setting.DisableWindowMinutes, time.Now())
	if !setting.DisableEnabled || operation_setting.ValidateFirstResponseDisableRate(setting.DisableRate) != nil ||
		!timedOut || counts.Total <= 0 || !common.AutomaticDisableChannelEnabled || !channelError.AutoBan {
		return
	}

	if !reachedFirstResponseTimeoutDisableThreshold(counts, setting) {
		return
	}
	rate := float64(counts.Timeouts) * 100 / float64(counts.Total)

	reason := fmt.Sprintf(
		"first response timeout rate %.2f%% (%d/%d attempts in %d minutes) reached configured threshold %.2f%%",
		rate,
		counts.Timeouts,
		counts.Total,
		setting.DisableWindowMinutes,
		setting.DisableRate,
	)
	DisableChannelWithEvent(channelError, model.ChannelStatusChange{
		Source:       "first_response_policy",
		ReasonCode:   "first_response_timeout_rate",
		ReasonDetail: reason,
		RequestId:    requestId,
	})
}
