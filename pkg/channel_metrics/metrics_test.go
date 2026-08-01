package channelmetrics

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAttemptMetricKeepsAttemptLocalTimingAndUsage(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	attempt := &Attempt{
		meta: AttemptMeta{
			ChannelId:  12,
			Group:      "premium",
			ModelName:  "gpt-test",
			Endpoint:   "/v1/responses?debug=secret",
			RetryIndex: 1,
			IsStream:   true,
		},
		startedAt:       startedAt,
		peakConcurrency: 3,
	}
	attempt.MarkFirstResponse(startedAt.Add(750 * time.Millisecond))
	attempt.SetOutputTokens(100)

	metric := attempt.metric(AttemptOutcome{Success: true, FirstResponseMonitored: true}, startedAt.Add(2750*time.Millisecond))

	assert.Equal(t, 12, metric.ChannelId)
	assert.Equal(t, "premium", metric.Group)
	assert.Equal(t, "gpt-test", metric.ModelName)
	assert.Equal(t, "/v1/responses", metric.Endpoint)
	assert.Equal(t, int64(1), metric.AttemptCount)
	assert.Equal(t, int64(1), metric.SuccessCount)
	assert.Equal(t, int64(1), metric.RetryCount)
	assert.Equal(t, int64(2750), metric.TotalLatencyMs)
	assert.Equal(t, int64(100), metric.OutputTokens)
	assert.Equal(t, int64(2000), metric.GenerationMs)
	assert.Equal(t, int64(1), metric.ThroughputSampleCount)
	assert.Equal(t, int64(3), metric.PeakConcurrency)
	assert.Equal(t, int64(1), metric.TtftLe1000Ms)
}

func TestAttemptMetricStoresOnlyBoundedErrorCodeAndHTTPStatus(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	attempt := &Attempt{
		meta:      AttemptMeta{ChannelId: 13, Group: "default", ModelName: "gpt-test", Endpoint: "/v1/responses"},
		startedAt: startedAt,
	}
	metric := attempt.metric(AttemptOutcome{
		ErrorClass: ErrorClass429,
		ErrorCode:  "rate_limit_exceeded",
		HTTPStatus: 429,
	}, startedAt.Add(time.Second))
	assert.Equal(t, "rate_limit_exceeded", metric.ErrorCode)
	assert.Equal(t, 429, metric.StatusCode)

	unsafeMetric := attempt.metric(AttemptOutcome{
		ErrorClass: ErrorClass5xx,
		ErrorCode:  "Bearer secret-value",
		HTTPStatus: 502,
	}, startedAt.Add(time.Second))
	assert.Equal(t, string(ErrorClassOther), unsafeMetric.ErrorCode)
	assert.Equal(t, 502, unsafeMetric.StatusCode)
}

func TestSummarizeMetricUsesMergeablePercentilesAndWeightedTPS(t *testing.T) {
	first := &model.ChannelMetric{
		AttemptCount:          5,
		SuccessCount:          4,
		OutputTokens:          100,
		GenerationMs:          1000,
		ThroughputSampleCount: 1,
		TtftLe100Ms:           1,
		TtftLe1000Ms:          4,
	}
	second := &model.ChannelMetric{
		AttemptCount:          5,
		SuccessCount:          5,
		OutputTokens:          100,
		GenerationMs:          9000,
		ThroughputSampleCount: 1,
		TtftLe1000Ms:          4,
		TtftOverflow:          1,
	}
	mergeMetric(first, second)

	summary := summarizeMetric(first)

	require.NotNil(t, summary.AverageTps)
	assert.Equal(t, 20.0, *summary.AverageTps)
	assert.Equal(t, int64(10), summary.TTFT.SampleCount)
	require.NotNil(t, summary.TTFT.P50Ms)
	require.NotNil(t, summary.TTFT.P95Ms)
	require.NotNil(t, summary.TTFT.P99Ms)
	assert.Equal(t, int64(1000), *summary.TTFT.P50Ms)
	assert.Equal(t, int64(120000), *summary.TTFT.P95Ms)
	assert.Equal(t, int64(120000), *summary.TTFT.P99Ms)
	assert.True(t, summary.TTFT.Overflow)
	assert.Equal(t, 90.0, summary.AvailabilityRate)
}

func setupChannelMetricTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ChannelMetric{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestRuntimeRedisMergesConcurrencyAndRPMAcrossManagers(t *testing.T) {
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = clientA.Close()
		_ = clientB.Close()
	})
	now := time.Unix(1_700_000_000, 0)
	managerA := &runtimeManager{now: func() time.Time { return now }, redisEnabled: func() bool { return true }, redisClient: func() *redis.Client { return clientA }}
	managerB := &runtimeManager{now: func() time.Time { return now }, redisEnabled: func() bool { return true }, redisClient: func() *redis.Client { return clientB }}

	leaseA, first := managerA.begin(context.Background(), 77)
	leaseB, second := managerB.begin(context.Background(), 77)

	assert.Equal(t, RuntimeModeRedis, first.Mode)
	assert.Equal(t, int64(1), first.CurrentConcurrency)
	assert.Equal(t, int64(2), second.CurrentConcurrency)
	assert.Equal(t, int64(2), second.RPM)
	leaseA.close()
	assert.Equal(t, int64(1), managerB.snapshot(context.Background(), 77).CurrentConcurrency)
	leaseB.close()
}

func TestRuntimeSnapshotDoesNotMutateRedisOrAllocateMemoryState(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	now := time.Unix(1_700_000_000, 0)
	manager := &runtimeManager{
		now:          func() time.Time { return now },
		redisEnabled: func() bool { return true },
		redisClient:  func() *redis.Client { return client },
	}
	channelId := 79
	require.NoError(t, client.ZAdd(t.Context(), runtimeActiveKey(channelId), &redis.Z{Score: float64(now.Add(-time.Second).UnixMilli()), Member: "expired-active"}).Err())
	require.NoError(t, client.ZAdd(t.Context(), runtimeRPMKey(channelId), &redis.Z{Score: float64(now.Add(-runtimeWindow).UnixMilli()), Member: "expired-rpm"}).Err())

	snapshot := manager.snapshot(t.Context(), channelId)

	assert.Equal(t, RuntimeModeRedis, snapshot.Mode)
	assert.Zero(t, snapshot.CurrentConcurrency)
	assert.Zero(t, snapshot.RPM)
	assert.Equal(t, int64(1), client.ZCard(t.Context(), runtimeActiveKey(channelId)).Val())
	assert.Equal(t, int64(1), client.ZCard(t.Context(), runtimeRPMKey(channelId)).Val())
	_, allocated := manager.memoryStates.Load(channelId)
	assert.False(t, allocated)
}

func TestRuntimeMemorySnapshotDoesNotAllocateState(t *testing.T) {
	manager := &runtimeManager{
		now:          time.Now,
		redisEnabled: func() bool { return false },
	}

	snapshot := manager.snapshot(t.Context(), 80)

	assert.Equal(t, RuntimeModeMemory, snapshot.Mode)
	assert.Zero(t, snapshot.CurrentConcurrency)
	assert.Zero(t, snapshot.RPM)
	_, allocated := manager.memoryStates.Load(80)
	assert.False(t, allocated)
}

func TestRuntimeFallbackIncludesLocalRedisAndFallbackLeases(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	now := time.Unix(1_700_000_000, 0)
	var currentClient *redis.Client = client
	clientLookups := 0
	manager := &runtimeManager{
		now:          func() time.Time { return now },
		redisEnabled: func() bool { return true },
		redisClient: func() *redis.Client {
			clientLookups++
			return currentClient
		},
	}

	redisLease, redisSnapshot := manager.begin(context.Background(), 88)
	require.Equal(t, RuntimeModeRedis, redisSnapshot.Mode)
	currentClient = nil
	fallbackSnapshot := manager.snapshot(context.Background(), 88)
	assert.Equal(t, RuntimeModeMemoryFallback, fallbackSnapshot.Mode)
	assert.Equal(t, int64(1), fallbackSnapshot.CurrentConcurrency)
	assert.Equal(t, int64(1), fallbackSnapshot.RPM)

	fallbackLease, fallbackBeginSnapshot := manager.begin(context.Background(), 88)
	require.Equal(t, RuntimeModeMemoryFallback, fallbackBeginSnapshot.Mode)
	assert.Equal(t, int64(2), fallbackBeginSnapshot.CurrentConcurrency)
	currentClient = client
	now = now.Add(runtimeFallbackProbeInterval + time.Second)
	recoveredSnapshot := manager.snapshot(context.Background(), 88)
	assert.Equal(t, RuntimeModeRedis, recoveredSnapshot.Mode)
	assert.Equal(t, int64(2), recoveredSnapshot.CurrentConcurrency)
	assert.Equal(t, int64(2), recoveredSnapshot.RPM)
	assert.Equal(t, 3, clientLookups)

	fallbackLease.close()
	redisLease.close()
}

func TestFlushPersistsQueuedMetrics(t *testing.T) {
	setupChannelMetricTestDB(t)
	const channelId = 99101
	RecordLocalSkip(AttemptMeta{
		ChannelId: channelId,
		Group:     strings.Repeat("g", 80),
		ModelName: "gpt-test",
		Endpoint:  "/v1/chat/completions?api_key=secret",
	}, SkipReasonConcurrency)
	require.NoError(t, Flush(t.Context()))

	rows, err := model.GetChannelMetricRows(model.ChannelMetricFilter{ChannelId: channelId}, model.ChannelMetricBucketMinute, 0, time.Now().Unix())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Len(t, []rune(rows[0].Group), 64)
	assert.Equal(t, "/v1/chat/completions", rows[0].Endpoint)
	assert.Equal(t, int64(1), rows[0].ConcurrencyRejectedCount)
}

func TestRetentionKeepsWholeMinuteBoundaryHour(t *testing.T) {
	setupChannelMetricTestDB(t)
	now := time.Date(2026, 8, 1, 20, 30, 0, 0, time.UTC)
	boundary := now.Add(-MinuteRetention).Truncate(time.Hour).Unix()
	for _, ts := range []int64{boundary - 60, boundary} {
		require.NoError(t, model.UpsertChannelMetric(&model.ChannelMetric{
			ChannelId:        99102,
			Group:            "default",
			ModelName:        "gpt-test",
			Endpoint:         "/v1/responses",
			BucketTs:         ts,
			BucketSeconds:    model.ChannelMetricBucketMinute,
			HistogramVersion: HistogramVersion,
			AttemptCount:     1,
		}))
	}

	cleanupRetainedMetrics(now)
	rows, err := model.GetChannelMetricRows(model.ChannelMetricFilter{ChannelId: 99102}, model.ChannelMetricBucketMinute, 0, now.Unix())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, boundary, rows[0].BucketTs)
}

func TestRepeatedRollupIncludesLateMetric(t *testing.T) {
	setupChannelMetricTestDB(t)
	const hourTs = int64(1_700_002_800)
	metric := &model.ChannelMetric{
		ChannelId:        99103,
		Group:            "default",
		ModelName:        "gpt-test",
		Endpoint:         "/v1/responses",
		BucketTs:         hourTs + 60,
		BucketSeconds:    model.ChannelMetricBucketMinute,
		HistogramVersion: HistogramVersion,
		AttemptCount:     1,
		Upstream5xxCount: 1,
		ErrorCode:        "bad_response_status_code",
		StatusCode:       502,
	}
	require.NoError(t, model.UpsertChannelMetric(metric))
	rollupHour(hourTs)
	metric.Id = 0
	metric.BucketTs = hourTs + 59*60
	require.NoError(t, model.UpsertChannelMetric(metric))
	rollupHour(hourTs)

	rows, err := model.GetChannelMetricRows(model.ChannelMetricFilter{ChannelId: metric.ChannelId}, model.ChannelMetricBucketHour, hourTs, hourTs)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(2), rows[0].AttemptCount)
	assert.Equal(t, int64(2), rows[0].Upstream5xxCount)
	assert.Equal(t, "bad_response_status_code", rows[0].ErrorCode)
	assert.Equal(t, 502, rows[0].StatusCode)
}
