package model

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ChannelMetricBucketMinute = 60
	ChannelMetricBucketHour   = 3600
)

var bearerCredentialPattern = regexp.MustCompile(`(?i)(bearer\s+)[^\s,;"']+`)

// ChannelMetric stores mergeable per-channel attempt counters. Minute and hour
// buckets share a table and are distinguished by BucketSeconds.
type ChannelMetric struct {
	Id               int64  `json:"id" gorm:"primaryKey"`
	ChannelId        int    `json:"channel_id" gorm:"uniqueIndex:idx_channel_metric_dimensions,priority:1;index:idx_channel_metric_channel_ts,priority:1"`
	DimensionsHash   string `json:"-" gorm:"size:64;uniqueIndex:idx_channel_metric_dimensions,priority:2"`
	Group            string `json:"group" gorm:"column:group;size:64"`
	ModelName        string `json:"model_name" gorm:"size:128"`
	Endpoint         string `json:"endpoint" gorm:"size:128"`
	ErrorCode        string `json:"error_code,omitempty" gorm:"size:64"`
	StatusCode       int    `json:"status_code,omitempty"`
	BucketTs         int64  `json:"bucket_ts" gorm:"uniqueIndex:idx_channel_metric_dimensions,priority:3;index:idx_channel_metric_channel_ts,priority:2;index:idx_channel_metric_bucket_ts"`
	BucketSeconds    int    `json:"bucket_seconds" gorm:"uniqueIndex:idx_channel_metric_dimensions,priority:4"`
	HistogramVersion int    `json:"histogram_version"`

	AttemptCount             int64 `json:"attempt_count"`
	SuccessCount             int64 `json:"success_count"`
	RetryCount               int64 `json:"retry_count"`
	FirstResponseMonitored   int64 `json:"first_response_monitored"`
	FirstResponseTimeout     int64 `json:"first_response_timeout"`
	Upstream429Count         int64 `json:"upstream_429_count" gorm:"column:upstream_429_count"`
	Upstream4xxCount         int64 `json:"upstream_4xx_count" gorm:"column:upstream_4xx_count"`
	Upstream5xxCount         int64 `json:"upstream_5xx_count" gorm:"column:upstream_5xx_count"`
	UpstreamTimeoutCount     int64 `json:"upstream_timeout_count"`
	CanceledCount            int64 `json:"canceled_count"`
	StreamErrorCount         int64 `json:"stream_error_count"`
	OtherErrorCount          int64 `json:"other_error_count"`
	CapacityRejectedCount    int64 `json:"capacity_rejected_count"`
	ConcurrencyRejectedCount int64 `json:"concurrency_rejected_count"`
	RPMRejectedCount         int64 `json:"rpm_rejected_count"`
	DisabledSkipCount        int64 `json:"disabled_skip_count"`
	CooldownSkipCount        int64 `json:"cooldown_skip_count"`
	TotalLatencyMs           int64 `json:"total_latency_ms"`
	OutputTokens             int64 `json:"output_tokens"`
	GenerationMs             int64 `json:"generation_ms"`
	ThroughputSampleCount    int64 `json:"throughput_sample_count"`
	PeakConcurrency          int64 `json:"peak_concurrency"`
	TtftLe100Ms              int64 `json:"ttft_le_100_ms" gorm:"column:ttft_le_100_ms"`
	TtftLe250Ms              int64 `json:"ttft_le_250_ms" gorm:"column:ttft_le_250_ms"`
	TtftLe500Ms              int64 `json:"ttft_le_500_ms" gorm:"column:ttft_le_500_ms"`
	TtftLe1000Ms             int64 `json:"ttft_le_1000_ms" gorm:"column:ttft_le_1000_ms"`
	TtftLe2000Ms             int64 `json:"ttft_le_2000_ms" gorm:"column:ttft_le_2000_ms"`
	TtftLe5000Ms             int64 `json:"ttft_le_5000_ms" gorm:"column:ttft_le_5000_ms"`
	TtftLe10000Ms            int64 `json:"ttft_le_10000_ms" gorm:"column:ttft_le_10000_ms"`
	TtftLe20000Ms            int64 `json:"ttft_le_20000_ms" gorm:"column:ttft_le_20000_ms"`
	TtftLe30000Ms            int64 `json:"ttft_le_30000_ms" gorm:"column:ttft_le_30000_ms"`
	TtftLe60000Ms            int64 `json:"ttft_le_60000_ms" gorm:"column:ttft_le_60000_ms"`
	TtftLe120000Ms           int64 `json:"ttft_le_120000_ms" gorm:"column:ttft_le_120000_ms"`
	TtftOverflow             int64 `json:"ttft_overflow"`
}

func (ChannelMetric) TableName() string {
	return "channel_metrics"
}

func setChannelMetricDimensionsHash(metric *ChannelMetric) {
	if metric == nil {
		return
	}
	hasher := sha256.New()
	var length [8]byte
	for _, dimension := range []string{metric.Group, metric.ModelName, metric.Endpoint, metric.ErrorCode} {
		binary.BigEndian.PutUint64(length[:], uint64(len(dimension)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(dimension))
	}
	binary.BigEndian.PutUint64(length[:], uint64(metric.StatusCode))
	_, _ = hasher.Write(length[:])
	metric.DimensionsHash = hex.EncodeToString(hasher.Sum(nil))
}

func UpsertChannelMetric(metric *ChannelMetric) error {
	return UpsertChannelMetricWithDB(DB, metric)
}

func UpsertChannelMetricWithDB(db *gorm.DB, metric *ChannelMetric) error {
	if metric == nil {
		return nil
	}
	if db == nil {
		return errors.New("channel metric database is not initialized")
	}
	if metric.BucketSeconds == 0 {
		metric.BucketSeconds = ChannelMetricBucketMinute
	}
	setChannelMetricDimensionsHash(metric)
	increment := func(column string, value int64) clause.Expr {
		return gorm.Expr("? + ?", clause.Column{Table: clause.CurrentTable, Name: column}, value)
	}
	maximum := func(column string, value int64) clause.Expr {
		target := clause.Column{Table: clause.CurrentTable, Name: column}
		return gorm.Expr("CASE WHEN ? < ? THEN ? ELSE ? END", target, value, value, target)
	}
	assignments := map[string]any{
		"attempt_count":              increment("attempt_count", metric.AttemptCount),
		"success_count":              increment("success_count", metric.SuccessCount),
		"retry_count":                increment("retry_count", metric.RetryCount),
		"first_response_monitored":   increment("first_response_monitored", metric.FirstResponseMonitored),
		"first_response_timeout":     increment("first_response_timeout", metric.FirstResponseTimeout),
		"upstream_429_count":         increment("upstream_429_count", metric.Upstream429Count),
		"upstream_4xx_count":         increment("upstream_4xx_count", metric.Upstream4xxCount),
		"upstream_5xx_count":         increment("upstream_5xx_count", metric.Upstream5xxCount),
		"upstream_timeout_count":     increment("upstream_timeout_count", metric.UpstreamTimeoutCount),
		"canceled_count":             increment("canceled_count", metric.CanceledCount),
		"stream_error_count":         increment("stream_error_count", metric.StreamErrorCount),
		"other_error_count":          increment("other_error_count", metric.OtherErrorCount),
		"capacity_rejected_count":    increment("capacity_rejected_count", metric.CapacityRejectedCount),
		"concurrency_rejected_count": increment("concurrency_rejected_count", metric.ConcurrencyRejectedCount),
		"rpm_rejected_count":         increment("rpm_rejected_count", metric.RPMRejectedCount),
		"disabled_skip_count":        increment("disabled_skip_count", metric.DisabledSkipCount),
		"cooldown_skip_count":        increment("cooldown_skip_count", metric.CooldownSkipCount),
		"total_latency_ms":           increment("total_latency_ms", metric.TotalLatencyMs),
		"output_tokens":              increment("output_tokens", metric.OutputTokens),
		"generation_ms":              increment("generation_ms", metric.GenerationMs),
		"throughput_sample_count":    increment("throughput_sample_count", metric.ThroughputSampleCount),
		"peak_concurrency":           maximum("peak_concurrency", metric.PeakConcurrency),
		"ttft_le_100_ms":             increment("ttft_le_100_ms", metric.TtftLe100Ms),
		"ttft_le_250_ms":             increment("ttft_le_250_ms", metric.TtftLe250Ms),
		"ttft_le_500_ms":             increment("ttft_le_500_ms", metric.TtftLe500Ms),
		"ttft_le_1000_ms":            increment("ttft_le_1000_ms", metric.TtftLe1000Ms),
		"ttft_le_2000_ms":            increment("ttft_le_2000_ms", metric.TtftLe2000Ms),
		"ttft_le_5000_ms":            increment("ttft_le_5000_ms", metric.TtftLe5000Ms),
		"ttft_le_10000_ms":           increment("ttft_le_10000_ms", metric.TtftLe10000Ms),
		"ttft_le_20000_ms":           increment("ttft_le_20000_ms", metric.TtftLe20000Ms),
		"ttft_le_30000_ms":           increment("ttft_le_30000_ms", metric.TtftLe30000Ms),
		"ttft_le_60000_ms":           increment("ttft_le_60000_ms", metric.TtftLe60000Ms),
		"ttft_le_120000_ms":          increment("ttft_le_120000_ms", metric.TtftLe120000Ms),
		"ttft_overflow":              increment("ttft_overflow", metric.TtftOverflow),
		"histogram_version":          metric.HistogramVersion,
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "channel_id"},
			{Name: "dimensions_hash"},
			{Name: "bucket_ts"},
			{Name: "bucket_seconds"},
		},
		DoUpdates: clause.Assignments(assignments),
	}).Create(metric).Error
}

type ChannelMetricFilter struct {
	ChannelId int
	Group     string
	ModelName string
	Endpoint  string
}

func applyChannelMetricFilter(query *gorm.DB, filter ChannelMetricFilter) *gorm.DB {
	if filter.ChannelId > 0 {
		query = query.Where("channel_id = ?", filter.ChannelId)
	}
	if filter.Group != "" {
		query = query.Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: filter.Group})
	}
	if filter.ModelName != "" {
		query = query.Where("model_name = ?", filter.ModelName)
	}
	if filter.Endpoint != "" {
		query = query.Where("endpoint = ?", filter.Endpoint)
	}
	return query
}

func GetChannelMetricRows(filter ChannelMetricFilter, bucketSeconds int, startTs int64, endTs int64) ([]ChannelMetric, error) {
	var rows []ChannelMetric
	query := DB.Model(&ChannelMetric{}).
		Where("bucket_seconds = ? AND bucket_ts >= ? AND bucket_ts <= ?", bucketSeconds, startTs, endTs)
	query = applyChannelMetricFilter(query, filter)
	err := query.Order("bucket_ts ASC, channel_id ASC").Find(&rows).Error
	return rows, err
}

func ListChannelMetricDimensionValues(filter ChannelMetricFilter, bucketSeconds int, startTs int64, endTs int64, dimension string) ([]string, error) {
	return listChannelMetricDimensionValuesWithDB(DB, filter, bucketSeconds, startTs, endTs, dimension)
}

func listChannelMetricDimensionValuesWithDB(db *gorm.DB, filter ChannelMetricFilter, bucketSeconds int, startTs int64, endTs int64, dimension string) ([]string, error) {
	column := ""
	switch dimension {
	case "group":
		column = "group"
	case "model":
		column = "model_name"
	case "endpoint":
		column = "endpoint"
	default:
		return nil, errors.New("invalid channel metric dimension")
	}
	var values []string
	query := db.Model(&ChannelMetric{}).
		Where("bucket_seconds = ? AND bucket_ts >= ? AND bucket_ts <= ?", bucketSeconds, startTs, endTs)
	query = applyChannelMetricFilter(query, filter)
	err := query.
		Distinct(column).
		Order(clause.OrderByColumn{Column: clause.Column{Name: column}}).
		Pluck(column, &values).Error
	return values, err
}

func DeleteChannelMetricsBefore(bucketSeconds int, cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return DB.Where("bucket_seconds = ? AND bucket_ts < ?", bucketSeconds, cutoffTs).Delete(&ChannelMetric{}).Error
}

func ReplaceChannelMetricHour(bucketTs int64, metrics []ChannelMetric) error {
	for index := range metrics {
		setChannelMetricDimensionsHash(&metrics[index])
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("bucket_seconds = ? AND bucket_ts = ?", ChannelMetricBucketHour, bucketTs).Delete(&ChannelMetric{}).Error; err != nil {
			return err
		}
		if len(metrics) == 0 {
			return nil
		}
		return tx.CreateInBatches(metrics, 100).Error
	})
}

func ListChannelsForMetrics() ([]Channel, error) {
	var channels []Channel
	err := DB.Select("id", "name", "type", "status", "setting").Order("id ASC").Find(&channels).Error
	return channels, err
}

func GetChannelForMetrics(channelId int) (Channel, error) {
	var channel Channel
	err := DB.Select("id", "name", "type", "status", "setting").Where("id = ?", channelId).First(&channel).Error
	return channel, err
}

// ChannelStatusEvent is the persistent audit trail for channel and multi-key
// status transitions. Key material is represented only by index and fingerprint.
type ChannelStatusEvent struct {
	Id                   int64  `json:"id" gorm:"primaryKey"`
	ChannelId            int    `json:"channel_id" gorm:"index:idx_channel_status_event_channel_ts,priority:1"`
	Scope                string `json:"scope" gorm:"size:16"`
	KeyIndex             *int   `json:"key_index,omitempty"`
	KeyFingerprint       string `json:"key_fingerprint,omitempty" gorm:"size:32"`
	FromStatus           int    `json:"from_status"`
	ToStatus             int    `json:"to_status"`
	ChannelStatusBefore  int    `json:"channel_status_before"`
	ChannelStatusAfter   int    `json:"channel_status_after"`
	Source               string `json:"source" gorm:"size:64"`
	ReasonCode           string `json:"reason_code" gorm:"size:64"`
	ReasonDetail         string `json:"reason_detail" gorm:"type:text"`
	PreviousReasonCode   string `json:"previous_reason_code,omitempty" gorm:"size:64"`
	PreviousReasonDetail string `json:"previous_reason_detail,omitempty" gorm:"type:text"`
	ActorUserId          int    `json:"actor_user_id,omitempty"`
	RequestId            string `json:"request_id,omitempty" gorm:"size:64"`
	CreatedAt            int64  `json:"created_at" gorm:"index:idx_channel_status_event_channel_ts,priority:2;index:idx_channel_status_event_created_at"`
}

func (ChannelStatusEvent) TableName() string {
	return "channel_status_events"
}

type ChannelStatusChange struct {
	Source                string
	ReasonCode            string
	ReasonDetail          string
	ActorUserId           int
	RequestId             string
	ExpectedStatusEventId *int64
}

func ChannelKeyFingerprint(key string) string {
	if key == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:8])
}

func SanitizeChannelStatusReason(reason string, sensitiveValue string) string {
	reason = common.RedactExactValue(reason, sensitiveValue)
	reason = bearerCredentialPattern.ReplaceAllString(reason, "${1}[REDACTED]")
	reason = common.MaskSensitiveInfo(strings.TrimSpace(reason))
	return truncateChannelStatusValue(reason, 512)
}

func NormalizeChannelStatusChange(change ChannelStatusChange, fallbackReason string) ChannelStatusChange {
	change.Source = strings.TrimSpace(change.Source)
	if change.Source == "" {
		change.Source = "legacy"
	}
	change.Source = truncateChannelStatusValue(change.Source, 64)
	change.ReasonCode = strings.TrimSpace(change.ReasonCode)
	if change.ReasonCode == "" {
		change.ReasonCode = "status_changed"
	}
	change.ReasonCode = truncateChannelStatusValue(change.ReasonCode, 64)
	change.RequestId = truncateChannelStatusValue(strings.TrimSpace(change.RequestId), 64)
	detail := strings.TrimSpace(change.ReasonDetail)
	if detail == "" {
		detail = strings.TrimSpace(fallbackReason)
	}
	change.ReasonDetail = SanitizeChannelStatusReason(detail, "")
	return change
}

func truncateChannelStatusValue(value string, maxRunes int) string {
	runes := []rune(value)
	if maxRunes > 0 && len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return value
}

func GetLatestChannelStatusEventId(channelId int) (int64, error) {
	var event ChannelStatusEvent
	result := DB.Select("id").Where("channel_id = ?", channelId).Order("id DESC").Limit(1).Find(&event)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, nil
	}
	return event.Id, nil
}

func GetLatestChannelStatusEvents(channelIds []int) (map[int]ChannelStatusEvent, error) {
	latest := make(map[int]ChannelStatusEvent)
	if len(channelIds) == 0 {
		return latest, nil
	}
	latestIds := DB.Model(&ChannelStatusEvent{}).
		Select("MAX(id)").
		Where("channel_id IN ?", channelIds).
		Group("channel_id")
	var events []ChannelStatusEvent
	if err := DB.Where("id IN (?)", latestIds).Find(&events).Error; err != nil {
		return nil, err
	}
	for _, event := range events {
		if _, exists := latest[event.ChannelId]; !exists {
			latest[event.ChannelId] = event
		}
	}
	return latest, nil
}

func ListChannelStatusEvents(channelId int, startTs int64, endTs int64, limit int) ([]ChannelStatusEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var events []ChannelStatusEvent
	query := DB.Model(&ChannelStatusEvent{}).Where("created_at >= ? AND created_at <= ?", startTs, endTs)
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&events).Error
	return events, err
}
