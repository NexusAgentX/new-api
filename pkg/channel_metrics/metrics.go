package channelmetrics

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

const recordQueueSize = 8192

type queuedMetric struct {
	metric *model.ChannelMetric
	write  func(*model.ChannelMetric) error
}

var (
	workerOnce     sync.Once
	retentionOnce  sync.Once
	pendingRecords sync.WaitGroup
	recordQueue    = make(chan queuedMetric, recordQueueSize)
)

func Init() {
	startRecordWorker()
	if common.IsMasterNode {
		retentionOnce.Do(func() { go retentionLoop() })
	}
}

func startRecordWorker() {
	workerOnce.Do(func() { go recordWorker() })
}

func BeginAttempt(ctx context.Context, meta AttemptMeta) *Attempt {
	if meta.ChannelId <= 0 || !perf_metrics_setting.GetSetting().Enabled {
		return nil
	}
	runtime, snapshot := defaultRuntimeManager.begin(ctx, meta.ChannelId)
	return &Attempt{
		meta:            meta,
		startedAt:       time.Now(),
		peakConcurrency: snapshot.CurrentConcurrency,
		runtime:         runtime,
	}
}

func (a *Attempt) Finish(outcome AttemptOutcome) {
	if a == nil || !a.finished.CompareAndSwap(false, true) {
		return
	}
	endedAt := time.Now()
	a.runtime.close()
	enqueueMetric(a.metric(outcome, endedAt))
}

func RecordLocalSkip(meta AttemptMeta, reason SkipReason) {
	if meta.ChannelId <= 0 || !perf_metrics_setting.GetSetting().Enabled {
		return
	}
	now := time.Now()
	metric := &model.ChannelMetric{
		ChannelId:        meta.ChannelId,
		Group:            normalizeDimension(meta.Group, "default", 64),
		ModelName:        normalizeDimension(meta.ModelName, "unknown", 128),
		Endpoint:         normalizeEndpoint(meta.Endpoint),
		BucketTs:         bucketStart(now.Unix(), model.ChannelMetricBucketMinute),
		BucketSeconds:    model.ChannelMetricBucketMinute,
		HistogramVersion: HistogramVersion,
	}
	switch reason {
	case SkipReasonConcurrency:
		metric.CapacityRejectedCount = 1
		metric.ConcurrencyRejectedCount = 1
	case SkipReasonRPM:
		metric.CapacityRejectedCount = 1
		metric.RPMRejectedCount = 1
	case SkipReasonDisabled:
		metric.DisabledSkipCount = 1
	case SkipReasonCooldown:
		metric.CooldownSkipCount = 1
	default:
		return
	}
	enqueueMetric(metric)
}

func GetRuntimeSnapshot(ctx context.Context, channelId int) RuntimeSnapshot {
	return defaultRuntimeManager.snapshot(ctx, channelId)
}

func Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		pendingRecords.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func enqueueMetric(metric *model.ChannelMetric) {
	if metric == nil || model.DB == nil {
		return
	}
	startRecordWorker()
	db := model.DB
	item := queuedMetric{
		metric: metric,
		write: func(value *model.ChannelMetric) error {
			return model.UpsertChannelMetricWithDB(db, value)
		},
	}
	pendingRecords.Add(1)
	select {
	case recordQueue <- item:
	default:
		// Preserve accounting during bursts. The synchronous fallback applies
		// backpressure only after the bounded asynchronous queue is exhausted.
		if err := item.write(item.metric); err != nil {
			common.SysError(fmt.Sprintf("record channel metric failed: channel=%d bucket=%d err=%v", metric.ChannelId, metric.BucketTs, err))
		}
		pendingRecords.Done()
	}
}

func recordWorker() {
	for item := range recordQueue {
		if err := item.write(item.metric); err != nil {
			common.SysError(fmt.Sprintf("record channel metric failed: channel=%d bucket=%d err=%v", item.metric.ChannelId, item.metric.BucketTs, err))
		}
		pendingRecords.Done()
	}
}

func normalizeDimension(value string, fallback string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	runes := []rune(value)
	if maxRunes > 0 && len(runes) > maxRunes {
		value = string(runes[:maxRunes])
	}
	return value
}

func normalizeErrorCode(value string, fallback ErrorClass) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = string(fallback)
	}
	if value == "" {
		value = string(ErrorClassOther)
	}
	if len(value) > 64 || strings.Contains(value, "sk-") || strings.HasPrefix(value, "bearer") {
		return string(ErrorClassOther)
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' || character == '.' || character == ':' {
			continue
		}
		return string(ErrorClassOther)
	}
	return value
}

func normalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if before, _, found := strings.Cut(endpoint, "?"); found {
		endpoint = before
	}
	if endpoint == "" {
		endpoint = "unknown"
	}
	runes := []rune(endpoint)
	if len(runes) > 128 {
		endpoint = string(runes[:128])
	}
	return endpoint
}
