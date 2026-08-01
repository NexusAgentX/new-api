package channelmetrics

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type rollupKey struct {
	channelId int
	group     string
	model     string
	endpoint  string
	errorCode string
	status    int
}

func retentionLoop() {
	rollupRecentHours(time.Now(), MinuteRetention)
	cleanupRetainedMetrics(time.Now())
	for {
		now := time.Now()
		next := now.Truncate(time.Hour).Add(time.Hour + 5*time.Minute)
		time.Sleep(time.Until(next))
		// Rebuild a trailing window so streams and task submissions that finish
		// after the first hourly rollup are eventually included.
		rollupRecentHours(time.Now(), 24*time.Hour)
		cleanupRetainedMetrics(time.Now())
	}
}

func rollupRecentHours(now time.Time, lookback time.Duration) {
	if lookback <= 0 || lookback > MinuteRetention {
		lookback = MinuteRetention
	}
	first := now.Add(-lookback).Truncate(time.Hour)
	last := now.Truncate(time.Hour).Add(-time.Hour)
	for hour := first; !hour.After(last); hour = hour.Add(time.Hour) {
		rollupHour(hour.Unix())
	}
}

func rollupHour(hourTs int64) {
	hourTs = bucketStart(hourTs, model.ChannelMetricBucketHour)
	rows, err := model.GetChannelMetricRows(model.ChannelMetricFilter{}, model.ChannelMetricBucketMinute, hourTs, hourTs+model.ChannelMetricBucketHour-1)
	if err != nil {
		common.SysError(fmt.Sprintf("load channel metric rollup hour %d failed: %v", hourTs, err))
		return
	}
	merged := make(map[rollupKey]*model.ChannelMetric)
	for index := range rows {
		row := rows[index]
		key := rollupKey{
			channelId: row.ChannelId,
			group:     row.Group,
			model:     row.ModelName,
			endpoint:  row.Endpoint,
			errorCode: row.ErrorCode,
			status:    row.StatusCode,
		}
		target := merged[key]
		if target == nil {
			target = &model.ChannelMetric{
				ChannelId:        row.ChannelId,
				Group:            row.Group,
				ModelName:        row.ModelName,
				Endpoint:         row.Endpoint,
				ErrorCode:        row.ErrorCode,
				StatusCode:       row.StatusCode,
				BucketTs:         hourTs,
				BucketSeconds:    model.ChannelMetricBucketHour,
				HistogramVersion: HistogramVersion,
			}
			merged[key] = target
		}
		mergeMetric(target, &row)
	}
	metrics := make([]model.ChannelMetric, 0, len(merged))
	for _, metric := range merged {
		metrics = append(metrics, *metric)
	}
	if err := model.ReplaceChannelMetricHour(hourTs, metrics); err != nil {
		common.SysError(fmt.Sprintf("replace channel metric rollup hour %d failed: %v", hourTs, err))
	}
}

func cleanupRetainedMetrics(now time.Time) {
	// Keep the entire boundary hour so queries can switch from hourly to
	// minute buckets without a gap or double counting.
	minuteCutoff := now.Add(-MinuteRetention).Truncate(time.Hour).Unix()
	if err := model.DeleteChannelMetricsBefore(model.ChannelMetricBucketMinute, minuteCutoff); err != nil {
		common.SysError("cleanup minute channel metrics failed: " + err.Error())
	}
	hourCutoff := now.Add(-HourRetention).Unix()
	if err := model.DeleteChannelMetricsBefore(model.ChannelMetricBucketHour, hourCutoff); err != nil {
		common.SysError("cleanup hourly channel metrics failed: " + err.Error())
	}
}

func mergeMetric(target *model.ChannelMetric, value *model.ChannelMetric) {
	target.AttemptCount += value.AttemptCount
	target.SuccessCount += value.SuccessCount
	target.RetryCount += value.RetryCount
	target.FirstResponseMonitored += value.FirstResponseMonitored
	target.FirstResponseTimeout += value.FirstResponseTimeout
	target.Upstream429Count += value.Upstream429Count
	target.Upstream4xxCount += value.Upstream4xxCount
	target.Upstream5xxCount += value.Upstream5xxCount
	target.UpstreamTimeoutCount += value.UpstreamTimeoutCount
	target.CanceledCount += value.CanceledCount
	target.StreamErrorCount += value.StreamErrorCount
	target.OtherErrorCount += value.OtherErrorCount
	target.CapacityRejectedCount += value.CapacityRejectedCount
	target.ConcurrencyRejectedCount += value.ConcurrencyRejectedCount
	target.RPMRejectedCount += value.RPMRejectedCount
	target.DisabledSkipCount += value.DisabledSkipCount
	target.CooldownSkipCount += value.CooldownSkipCount
	target.TotalLatencyMs += value.TotalLatencyMs
	target.OutputTokens += value.OutputTokens
	target.GenerationMs += value.GenerationMs
	target.ThroughputSampleCount += value.ThroughputSampleCount
	if value.PeakConcurrency > target.PeakConcurrency {
		target.PeakConcurrency = value.PeakConcurrency
	}
	target.TtftLe100Ms += value.TtftLe100Ms
	target.TtftLe250Ms += value.TtftLe250Ms
	target.TtftLe500Ms += value.TtftLe500Ms
	target.TtftLe1000Ms += value.TtftLe1000Ms
	target.TtftLe2000Ms += value.TtftLe2000Ms
	target.TtftLe5000Ms += value.TtftLe5000Ms
	target.TtftLe10000Ms += value.TtftLe10000Ms
	target.TtftLe20000Ms += value.TtftLe20000Ms
	target.TtftLe30000Ms += value.TtftLe30000Ms
	target.TtftLe60000Ms += value.TtftLe60000Ms
	target.TtftLe120000Ms += value.TtftLe120000Ms
	target.TtftOverflow += value.TtftOverflow
}
