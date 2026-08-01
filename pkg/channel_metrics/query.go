package channelmetrics

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

type QueryParams struct {
	ChannelId int
	Group     string
	ModelName string
	Endpoint  string
	Hours     int
	StartTs   int64
	EndTs     int64
}

type Percentiles struct {
	SampleCount int64  `json:"sample_count"`
	P50Ms       *int64 `json:"p50_ms"`
	P95Ms       *int64 `json:"p95_ms"`
	P99Ms       *int64 `json:"p99_ms"`
	Overflow    bool   `json:"overflow"`
}

type ErrorBreakdownItem struct {
	HTTPStatus int    `json:"http_status"`
	ErrorCode  string `json:"error_code"`
	Count      int64  `json:"count"`
}

type MetricSummary struct {
	AttemptCount               int64       `json:"attempt_count"`
	RPM                        float64     `json:"rpm"`
	SuccessCount               int64       `json:"success_count"`
	ErrorCount                 int64       `json:"error_count"`
	RetryCount                 int64       `json:"retry_count"`
	AvailabilityRate           float64     `json:"availability_rate"`
	ErrorRate                  float64     `json:"error_rate"`
	FirstResponseMonitored     int64       `json:"first_response_monitored"`
	FirstResponseTimeout       int64       `json:"first_response_timeout"`
	FirstResponseInterceptRate float64     `json:"first_response_intercept_rate"`
	AverageLatencyMs           int64       `json:"average_latency_ms"`
	AverageTps                 *float64    `json:"average_tps"`
	ThroughputSampleCount      int64       `json:"throughput_sample_count"`
	TTFT                       Percentiles `json:"ttft"`
	Upstream429Count           int64       `json:"upstream_429_count"`
	Upstream4xxCount           int64       `json:"upstream_4xx_count"`
	Upstream5xxCount           int64       `json:"upstream_5xx_count"`
	UpstreamTimeoutCount       int64       `json:"upstream_timeout_count"`
	CanceledCount              int64       `json:"canceled_count"`
	StreamErrorCount           int64       `json:"stream_error_count"`
	OtherErrorCount            int64       `json:"other_error_count"`
	CapacityRejectedCount      int64       `json:"capacity_rejected_count"`
	ConcurrencyRejectedCount   int64       `json:"concurrency_rejected_count"`
	RPMRejectedCount           int64       `json:"rpm_rejected_count"`
	DisabledSkipCount          int64       `json:"disabled_skip_count"`
	CooldownSkipCount          int64       `json:"cooldown_skip_count"`
	PeakConcurrency            int64       `json:"peak_concurrency"`
}

type OverviewItem struct {
	ChannelId         int                       `json:"channel_id"`
	ChannelName       string                    `json:"channel_name"`
	ChannelType       int                       `json:"channel_type"`
	Status            int                       `json:"status"`
	Metrics           MetricSummary             `json:"metrics"`
	LatestStatusEvent *model.ChannelStatusEvent `json:"latest_status_event"`
}

type OverviewResult struct {
	Enabled             bool           `json:"enabled"`
	StartTs             int64          `json:"start_ts"`
	EndTs               int64          `json:"end_ts"`
	MinuteRetentionDays int            `json:"minute_retention_days"`
	HourRetentionDays   int            `json:"hour_retention_days"`
	Items               []OverviewItem `json:"items"`
}

type SeriesPoint struct {
	Ts            int64         `json:"ts"`
	BucketSeconds int           `json:"bucket_seconds"`
	RuntimePeak   int64         `json:"peak_concurrency"`
	Metrics       MetricSummary `json:"metrics"`
}

type DetailResult struct {
	Enabled             bool                       `json:"enabled"`
	StartTs             int64                      `json:"start_ts"`
	EndTs               int64                      `json:"end_ts"`
	MinuteRetentionDays int                        `json:"minute_retention_days"`
	HourRetentionDays   int                        `json:"hour_retention_days"`
	ChannelId           int                        `json:"channel_id"`
	ChannelName         string                     `json:"channel_name"`
	ChannelType         int                        `json:"channel_type"`
	Status              int                        `json:"status"`
	Summary             MetricSummary              `json:"summary"`
	Series              []SeriesPoint              `json:"series"`
	ErrorBreakdown      []ErrorBreakdownItem       `json:"error_breakdown"`
	StatusEvents        []model.ChannelStatusEvent `json:"status_events"`
}

type RuntimeItem struct {
	ChannelId      int             `json:"channel_id"`
	ChannelName    string          `json:"channel_name"`
	ChannelType    int             `json:"channel_type"`
	Status         int             `json:"status"`
	MaxConcurrency int             `json:"max_concurrency"`
	RPMLimit       int             `json:"rpm_limit"`
	Runtime        RuntimeSnapshot `json:"runtime"`
}

type RuntimeResult struct {
	Enabled bool          `json:"enabled"`
	AsOfTs  int64         `json:"as_of_ts"`
	Items   []RuntimeItem `json:"items"`
}

type DimensionResult struct {
	StartTs   int64    `json:"start_ts"`
	EndTs     int64    `json:"end_ts"`
	Groups    []string `json:"groups"`
	Models    []string `json:"models"`
	Endpoints []string `json:"endpoints"`
}

type metricBucketRange struct {
	bucketSeconds int
	startTs       int64
	endTs         int64
}

type errorBreakdownKey struct {
	statusCode int
	errorCode  string
}

func QueryOverview(ctx context.Context, params QueryParams) (OverviewResult, error) {
	startTs, endTs := normalizeRange(params)
	params.StartTs = startTs
	params.EndTs = endTs
	rows, err := loadMetricRows(params)
	if err != nil {
		return OverviewResult{}, err
	}
	byChannel := make(map[int]*model.ChannelMetric)
	for index := range rows {
		row := rows[index]
		total := byChannel[row.ChannelId]
		if total == nil {
			total = &model.ChannelMetric{ChannelId: row.ChannelId, HistogramVersion: HistogramVersion}
			byChannel[row.ChannelId] = total
		}
		mergeMetric(total, &row)
	}
	channels, err := model.ListChannelsForMetrics()
	if err != nil {
		return OverviewResult{}, err
	}
	channelIds := make([]int, 0, len(channels))
	for _, channel := range channels {
		if params.ChannelId == 0 || channel.Id == params.ChannelId {
			channelIds = append(channelIds, channel.Id)
		}
	}
	latestEvents, err := model.GetLatestChannelStatusEvents(channelIds)
	if err != nil {
		return OverviewResult{}, err
	}
	items := make([]OverviewItem, 0, len(channelIds))
	for _, channel := range channels {
		if params.ChannelId > 0 && channel.Id != params.ChannelId {
			continue
		}
		metrics := summarizeMetric(byChannel[channel.Id])
		metrics.RPM = attemptsPerMinute(metrics.AttemptCount, endTs-startTs)
		item := OverviewItem{
			ChannelId:   channel.Id,
			ChannelName: channel.Name,
			ChannelType: channel.Type,
			Status:      channel.Status,
			Metrics:     metrics,
		}
		if event, exists := latestEvents[channel.Id]; exists {
			eventCopy := event
			item.LatestStatusEvent = &eventCopy
		}
		items = append(items, item)
	}
	return OverviewResult{
		Enabled:             perf_metrics_setting.GetSetting().Enabled,
		StartTs:             startTs,
		EndTs:               endTs,
		MinuteRetentionDays: int(MinuteRetention / (24 * time.Hour)),
		HourRetentionDays:   int(HourRetention / (24 * time.Hour)),
		Items:               items,
	}, nil
}

func QueryDetail(ctx context.Context, params QueryParams) (DetailResult, error) {
	channel, err := model.GetChannelForMetrics(params.ChannelId)
	if err != nil {
		return DetailResult{}, err
	}
	startTs, endTs := normalizeRange(params)
	params.StartTs = startTs
	params.EndTs = endTs
	rows, err := loadMetricRows(params)
	if err != nil {
		return DetailResult{}, err
	}
	byBucket := make(map[int64]*model.ChannelMetric)
	errorCounts := make(map[errorBreakdownKey]int64)
	total := &model.ChannelMetric{ChannelId: params.ChannelId, HistogramVersion: HistogramVersion}
	for index := range rows {
		row := rows[index]
		mergeMetric(total, &row)
		errorCount := row.AttemptCount - row.SuccessCount
		if errorCount > 0 {
			errorCode := row.ErrorCode
			if errorCode == "" {
				errorCode = string(ErrorClassOther)
			}
			errorCounts[errorBreakdownKey{statusCode: row.StatusCode, errorCode: errorCode}] += errorCount
		}
		bucket := byBucket[row.BucketTs]
		if bucket == nil {
			bucket = &model.ChannelMetric{ChannelId: params.ChannelId, BucketTs: row.BucketTs, BucketSeconds: row.BucketSeconds, HistogramVersion: HistogramVersion}
			byBucket[row.BucketTs] = bucket
		}
		mergeMetric(bucket, &row)
	}
	timestamps := make([]int64, 0, len(byBucket))
	for ts := range byBucket {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	series := make([]SeriesPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		bucket := byBucket[ts]
		metrics := summarizeMetric(bucket)
		metrics.RPM = attemptsPerMinute(metrics.AttemptCount, int64(bucket.BucketSeconds))
		series = append(series, SeriesPoint{Ts: ts, BucketSeconds: bucket.BucketSeconds, RuntimePeak: bucket.PeakConcurrency, Metrics: metrics})
	}
	errorBreakdown := make([]ErrorBreakdownItem, 0, len(errorCounts))
	for key, count := range errorCounts {
		errorBreakdown = append(errorBreakdown, ErrorBreakdownItem{
			HTTPStatus: key.statusCode,
			ErrorCode:  key.errorCode,
			Count:      count,
		})
	}
	sort.Slice(errorBreakdown, func(i, j int) bool {
		if errorBreakdown[i].Count != errorBreakdown[j].Count {
			return errorBreakdown[i].Count > errorBreakdown[j].Count
		}
		if errorBreakdown[i].HTTPStatus != errorBreakdown[j].HTTPStatus {
			return errorBreakdown[i].HTTPStatus < errorBreakdown[j].HTTPStatus
		}
		return errorBreakdown[i].ErrorCode < errorBreakdown[j].ErrorCode
	})
	events, err := model.ListChannelStatusEvents(params.ChannelId, startTs, endTs, 500)
	if err != nil {
		return DetailResult{}, err
	}
	summary := summarizeMetric(total)
	summary.RPM = attemptsPerMinute(summary.AttemptCount, endTs-startTs)
	return DetailResult{
		Enabled:             perf_metrics_setting.GetSetting().Enabled,
		StartTs:             startTs,
		EndTs:               endTs,
		MinuteRetentionDays: int(MinuteRetention / (24 * time.Hour)),
		HourRetentionDays:   int(HourRetention / (24 * time.Hour)),
		ChannelId:           params.ChannelId,
		ChannelName:         channel.Name,
		ChannelType:         channel.Type,
		Status:              channel.Status,
		Summary:             summary,
		Series:              series,
		ErrorBreakdown:      errorBreakdown,
		StatusEvents:        events,
	}, nil
}

func QueryDimensions(params QueryParams) (DimensionResult, error) {
	startTs, endTs := normalizeRange(params)
	params.StartTs = startTs
	params.EndTs = endTs
	filter := model.ChannelMetricFilter{ChannelId: params.ChannelId}
	result := DimensionResult{StartTs: startTs, EndTs: endTs}
	destinations := map[string]*[]string{
		"group":    &result.Groups,
		"model":    &result.Models,
		"endpoint": &result.Endpoints,
	}
	for dimension, destination := range destinations {
		seen := make(map[string]struct{})
		for _, queryRange := range channelMetricQueryRanges(params) {
			values, err := model.ListChannelMetricDimensionValues(
				filter,
				queryRange.bucketSeconds,
				queryRange.startTs,
				queryRange.endTs,
				dimension,
			)
			if err != nil {
				return DimensionResult{}, err
			}
			for _, value := range values {
				if value != "" {
					seen[value] = struct{}{}
				}
			}
		}
		values := make([]string, 0, len(seen))
		for value := range seen {
			values = append(values, value)
		}
		sort.Strings(values)
		*destination = values
	}
	return result, nil
}

func QueryRuntime(ctx context.Context, channelId int) (RuntimeResult, error) {
	channels, err := model.ListChannelsForMetrics()
	if err != nil {
		return RuntimeResult{}, err
	}
	items := make([]RuntimeItem, 0, len(channels))
	for index := range channels {
		channel := &channels[index]
		if channelId > 0 && channel.Id != channelId {
			continue
		}
		settings := dto.ChannelSettings{}
		if channel.Setting != nil && *channel.Setting != "" {
			if err := common.Unmarshal([]byte(*channel.Setting), &settings); err != nil {
				common.SysError("parse channel settings for runtime metrics failed: " + err.Error())
			}
		}
		items = append(items, RuntimeItem{
			ChannelId:      channel.Id,
			ChannelName:    channel.Name,
			ChannelType:    channel.Type,
			Status:         channel.Status,
			MaxConcurrency: settings.MaxConcurrency,
			RPMLimit:       settings.RPMLimit,
			Runtime:        GetRuntimeSnapshot(ctx, channel.Id),
		})
	}
	return RuntimeResult{
		Enabled: perf_metrics_setting.GetSetting().Enabled,
		AsOfTs:  time.Now().Unix(),
		Items:   items,
	}, nil
}

func normalizeRange(params QueryParams) (int64, int64) {
	endTs := params.EndTs
	if endTs <= 0 || endTs > time.Now().Add(time.Minute).Unix() {
		endTs = time.Now().Unix()
	}
	startTs := params.StartTs
	if startTs <= 0 {
		hours := params.Hours
		if hours <= 0 {
			hours = 24
		}
		if hours > int(HourRetention/time.Hour) {
			hours = int(HourRetention / time.Hour)
		}
		startTs = endTs - int64(hours)*3600
	}
	oldest := endTs - int64(HourRetention/time.Second)
	if startTs < oldest {
		startTs = oldest
	}
	if startTs > endTs {
		startTs = endTs
	}
	return startTs, endTs
}

func loadMetricRows(params QueryParams) ([]model.ChannelMetric, error) {
	filter := model.ChannelMetricFilter{ChannelId: params.ChannelId, Group: params.Group, ModelName: params.ModelName, Endpoint: normalizeEndpoint(params.Endpoint)}
	if params.Endpoint == "" {
		filter.Endpoint = ""
	}
	rows := make([]model.ChannelMetric, 0)
	for _, queryRange := range channelMetricQueryRanges(params) {
		bucketRows, err := model.GetChannelMetricRows(filter, queryRange.bucketSeconds, queryRange.startTs, queryRange.endTs)
		if err != nil {
			return nil, err
		}
		rows = append(rows, bucketRows...)
	}
	return rows, nil
}

func channelMetricQueryRanges(params QueryParams) []metricBucketRange {
	minuteBoundary := bucketStart(time.Now().Add(-MinuteRetention).Unix(), model.ChannelMetricBucketHour)
	ranges := make([]metricBucketRange, 0, 2)
	if params.StartTs < minuteBoundary {
		hourEnd := params.EndTs
		if hourEnd >= minuteBoundary {
			hourEnd = minuteBoundary - 1
		}
		if hourEnd >= params.StartTs {
			ranges = append(ranges, metricBucketRange{
				bucketSeconds: model.ChannelMetricBucketHour,
				startTs:       params.StartTs,
				endTs:         hourEnd,
			})
		}
	}
	minuteStart := params.StartTs
	if minuteStart < minuteBoundary {
		minuteStart = minuteBoundary
	}
	if minuteStart <= params.EndTs {
		ranges = append(ranges, metricBucketRange{
			bucketSeconds: model.ChannelMetricBucketMinute,
			startTs:       minuteStart,
			endTs:         params.EndTs,
		})
	}
	return ranges
}

func summarizeMetric(metric *model.ChannelMetric) MetricSummary {
	if metric == nil {
		return MetricSummary{TTFT: Percentiles{}}
	}
	errorCount := metric.AttemptCount - metric.SuccessCount
	if errorCount < 0 {
		errorCount = 0
	}
	summary := MetricSummary{
		AttemptCount:               metric.AttemptCount,
		SuccessCount:               metric.SuccessCount,
		ErrorCount:                 errorCount,
		RetryCount:                 metric.RetryCount,
		AvailabilityRate:           percent(metric.SuccessCount, metric.AttemptCount),
		ErrorRate:                  percent(errorCount, metric.AttemptCount),
		FirstResponseMonitored:     metric.FirstResponseMonitored,
		FirstResponseTimeout:       metric.FirstResponseTimeout,
		FirstResponseInterceptRate: percent(metric.FirstResponseTimeout, metric.FirstResponseMonitored),
		Upstream429Count:           metric.Upstream429Count,
		Upstream4xxCount:           metric.Upstream4xxCount,
		Upstream5xxCount:           metric.Upstream5xxCount,
		UpstreamTimeoutCount:       metric.UpstreamTimeoutCount,
		CanceledCount:              metric.CanceledCount,
		StreamErrorCount:           metric.StreamErrorCount,
		OtherErrorCount:            metric.OtherErrorCount,
		CapacityRejectedCount:      metric.CapacityRejectedCount,
		ConcurrencyRejectedCount:   metric.ConcurrencyRejectedCount,
		RPMRejectedCount:           metric.RPMRejectedCount,
		DisabledSkipCount:          metric.DisabledSkipCount,
		CooldownSkipCount:          metric.CooldownSkipCount,
		ThroughputSampleCount:      metric.ThroughputSampleCount,
		PeakConcurrency:            metric.PeakConcurrency,
		TTFT:                       metricPercentiles(metric),
	}
	if metric.AttemptCount > 0 {
		summary.AverageLatencyMs = metric.TotalLatencyMs / metric.AttemptCount
	}
	if metric.ThroughputSampleCount > 0 && metric.GenerationMs > 0 {
		value := math.Round((float64(metric.OutputTokens)/(float64(metric.GenerationMs)/1000))*100) / 100
		summary.AverageTps = &value
	}
	return summary
}

func metricPercentiles(metric *model.ChannelMetric) Percentiles {
	buckets := []int64{
		metric.TtftLe100Ms,
		metric.TtftLe250Ms,
		metric.TtftLe500Ms,
		metric.TtftLe1000Ms,
		metric.TtftLe2000Ms,
		metric.TtftLe5000Ms,
		metric.TtftLe10000Ms,
		metric.TtftLe20000Ms,
		metric.TtftLe30000Ms,
		metric.TtftLe60000Ms,
		metric.TtftLe120000Ms,
		metric.TtftOverflow,
	}
	total := int64(0)
	for _, count := range buckets {
		total += count
	}
	result := Percentiles{SampleCount: total, Overflow: metric.TtftOverflow > 0}
	if total == 0 {
		return result
	}
	result.P50Ms = percentileFromBuckets(buckets, total, 0.50)
	result.P95Ms = percentileFromBuckets(buckets, total, 0.95)
	result.P99Ms = percentileFromBuckets(buckets, total, 0.99)
	return result
}

func percentileFromBuckets(buckets []int64, total int64, percentile float64) *int64 {
	target := int64(math.Ceil(float64(total) * percentile))
	seen := int64(0)
	for index, count := range buckets {
		seen += count
		if seen < target {
			continue
		}
		value := ttftBoundsMs[len(ttftBoundsMs)-1]
		if index < len(ttftBoundsMs) {
			value = ttftBoundsMs[index]
		}
		return &value
	}
	return nil
}

func attemptsPerMinute(attempts int64, durationSeconds int64) float64 {
	if attempts <= 0 || durationSeconds <= 0 {
		return 0
	}
	return math.Round((float64(attempts)*60/float64(durationSeconds))*100) / 100
}

func percent(numerator int64, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return math.Round((float64(numerator)/float64(denominator)*100)*100) / 100
}
