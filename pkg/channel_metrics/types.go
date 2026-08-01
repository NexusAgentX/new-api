package channelmetrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const (
	HistogramVersion = 1
	MinuteRetention  = 7 * 24 * time.Hour
	HourRetention    = 90 * 24 * time.Hour
)

var ttftBoundsMs = [...]int64{100, 250, 500, 1000, 2000, 5000, 10000, 20000, 30000, 60000, 120000}

type ErrorClass string

const (
	ErrorClassNone     ErrorClass = ""
	ErrorClass429      ErrorClass = "upstream_429"
	ErrorClass4xx      ErrorClass = "upstream_4xx"
	ErrorClass5xx      ErrorClass = "upstream_5xx"
	ErrorClassTimeout  ErrorClass = "upstream_timeout"
	ErrorClassCanceled ErrorClass = "canceled"
	ErrorClassStream   ErrorClass = "stream_error"
	ErrorClassOther    ErrorClass = "other_error"
)

type SkipReason string

const (
	SkipReasonConcurrency SkipReason = "capacity_concurrency"
	SkipReasonRPM         SkipReason = "capacity_rpm"
	SkipReasonDisabled    SkipReason = "disabled"
	SkipReasonCooldown    SkipReason = "cooldown"
)

type AttemptMeta struct {
	ChannelId  int
	Group      string
	ModelName  string
	Endpoint   string
	RetryIndex int
	IsStream   bool
}

type AttemptOutcome struct {
	Success                bool
	ErrorClass             ErrorClass
	ErrorCode              string
	HTTPStatus             int
	FirstResponseMonitored bool
	FirstResponseTimedOut  bool
}

type Attempt struct {
	meta            AttemptMeta
	startedAt       time.Time
	firstResponseAt time.Time
	outputTokens    int64
	hasOutputTokens bool
	peakConcurrency int64
	runtime         *runtimeLease
	mu              sync.Mutex
	finished        atomic.Bool
}

func (a *Attempt) MarkFirstResponse(at time.Time) {
	if a == nil || at.IsZero() {
		return
	}
	a.mu.Lock()
	if !a.finished.Load() && a.firstResponseAt.IsZero() {
		a.firstResponseAt = at
	}
	a.mu.Unlock()
}

func (a *Attempt) HasFirstResponse() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	hasFirstResponse := !a.firstResponseAt.IsZero()
	a.mu.Unlock()
	return hasFirstResponse
}

func (a *Attempt) SetOutputTokens(tokens int64) {
	if a == nil || tokens < 0 {
		return
	}
	a.mu.Lock()
	if !a.finished.Load() {
		a.outputTokens = tokens
		a.hasOutputTokens = true
	}
	a.mu.Unlock()
}

func (a *Attempt) metric(outcome AttemptOutcome, endedAt time.Time) *model.ChannelMetric {
	a.mu.Lock()
	firstResponseAt := a.firstResponseAt
	outputTokens := a.outputTokens
	hasOutputTokens := a.hasOutputTokens
	a.mu.Unlock()

	latencyMs := endedAt.Sub(a.startedAt).Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}
	metric := &model.ChannelMetric{
		ChannelId:        a.meta.ChannelId,
		Group:            normalizeDimension(a.meta.Group, "default", 64),
		ModelName:        normalizeDimension(a.meta.ModelName, "unknown", 128),
		Endpoint:         normalizeEndpoint(a.meta.Endpoint),
		BucketTs:         bucketStart(a.startedAt.Unix(), model.ChannelMetricBucketMinute),
		BucketSeconds:    model.ChannelMetricBucketMinute,
		HistogramVersion: HistogramVersion,
		AttemptCount:     1,
		TotalLatencyMs:   latencyMs,
		PeakConcurrency:  a.peakConcurrency,
	}
	if outcome.Success {
		metric.SuccessCount = 1
	} else {
		metric.ErrorCode = normalizeErrorCode(outcome.ErrorCode, outcome.ErrorClass)
		if outcome.HTTPStatus >= 100 && outcome.HTTPStatus <= 599 {
			metric.StatusCode = outcome.HTTPStatus
		}
	}
	if a.meta.RetryIndex > 0 {
		metric.RetryCount = 1
	}
	if outcome.FirstResponseMonitored {
		metric.FirstResponseMonitored = 1
	}
	if outcome.FirstResponseTimedOut {
		metric.FirstResponseTimeout = 1
	}

	switch outcome.ErrorClass {
	case ErrorClass429:
		metric.Upstream429Count = 1
	case ErrorClass4xx:
		metric.Upstream4xxCount = 1
	case ErrorClass5xx:
		metric.Upstream5xxCount = 1
	case ErrorClassTimeout:
		metric.UpstreamTimeoutCount = 1
	case ErrorClassCanceled:
		metric.CanceledCount = 1
	case ErrorClassStream:
		metric.StreamErrorCount = 1
	case ErrorClassOther:
		metric.OtherErrorCount = 1
	}

	if a.meta.IsStream && !firstResponseAt.IsZero() && !firstResponseAt.Before(a.startedAt) {
		ttftMs := firstResponseAt.Sub(a.startedAt).Milliseconds()
		setTTFTBucket(metric, ttftMs)
	}
	if hasOutputTokens {
		generationMs := latencyMs
		if a.meta.IsStream && !firstResponseAt.IsZero() && endedAt.After(firstResponseAt) {
			generationMs = endedAt.Sub(firstResponseAt).Milliseconds()
		}
		if outputTokens >= 0 && generationMs > 0 {
			metric.OutputTokens = outputTokens
			metric.GenerationMs = generationMs
			metric.ThroughputSampleCount = 1
		}
	}
	return metric
}

func setTTFTBucket(metric *model.ChannelMetric, ttftMs int64) {
	switch {
	case ttftMs <= ttftBoundsMs[0]:
		metric.TtftLe100Ms = 1
	case ttftMs <= ttftBoundsMs[1]:
		metric.TtftLe250Ms = 1
	case ttftMs <= ttftBoundsMs[2]:
		metric.TtftLe500Ms = 1
	case ttftMs <= ttftBoundsMs[3]:
		metric.TtftLe1000Ms = 1
	case ttftMs <= ttftBoundsMs[4]:
		metric.TtftLe2000Ms = 1
	case ttftMs <= ttftBoundsMs[5]:
		metric.TtftLe5000Ms = 1
	case ttftMs <= ttftBoundsMs[6]:
		metric.TtftLe10000Ms = 1
	case ttftMs <= ttftBoundsMs[7]:
		metric.TtftLe20000Ms = 1
	case ttftMs <= ttftBoundsMs[8]:
		metric.TtftLe30000Ms = 1
	case ttftMs <= ttftBoundsMs[9]:
		metric.TtftLe60000Ms = 1
	case ttftMs <= ttftBoundsMs[10]:
		metric.TtftLe120000Ms = 1
	default:
		metric.TtftOverflow = 1
	}
}

func bucketStart(ts int64, seconds int) int64 {
	if seconds <= 0 {
		seconds = model.ChannelMetricBucketMinute
	}
	return ts - ts%int64(seconds)
}
