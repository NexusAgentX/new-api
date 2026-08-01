package model

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestUpsertChannelMetricMergesCountersAndPeak(t *testing.T) {
	const channelId = 99001
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id = ?", channelId).Delete(&ChannelMetric{}).Error)
	})
	first := &ChannelMetric{
		ChannelId:             channelId,
		Group:                 "default",
		ModelName:             "gpt-test",
		Endpoint:              "/v1/responses",
		BucketTs:              1_700_000_000,
		BucketSeconds:         ChannelMetricBucketMinute,
		HistogramVersion:      1,
		AttemptCount:          1,
		SuccessCount:          1,
		OutputTokens:          100,
		GenerationMs:          2000,
		ThroughputSampleCount: 1,
		PeakConcurrency:       4,
		TtftLe1000Ms:          1,
	}
	second := &ChannelMetric{
		ChannelId:             channelId,
		Group:                 "default",
		ModelName:             "gpt-test",
		Endpoint:              "/v1/responses",
		BucketTs:              1_700_000_000,
		BucketSeconds:         ChannelMetricBucketMinute,
		HistogramVersion:      1,
		AttemptCount:          1,
		Upstream429Count:      1,
		OutputTokens:          50,
		GenerationMs:          1000,
		ThroughputSampleCount: 1,
		PeakConcurrency:       2,
		TtftLe2000Ms:          1,
	}

	require.NoError(t, UpsertChannelMetric(first))
	require.NoError(t, UpsertChannelMetric(second))
	rows, err := GetChannelMetricRows(ChannelMetricFilter{ChannelId: channelId}, ChannelMetricBucketMinute, first.BucketTs, first.BucketTs)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	assert.Equal(t, int64(2), rows[0].AttemptCount)
	assert.Equal(t, int64(1), rows[0].SuccessCount)
	assert.Equal(t, int64(1), rows[0].Upstream429Count)
	assert.Equal(t, int64(150), rows[0].OutputTokens)
	assert.Equal(t, int64(3000), rows[0].GenerationMs)
	assert.Equal(t, int64(2), rows[0].ThroughputSampleCount)
	assert.Equal(t, int64(4), rows[0].PeakConcurrency)
	assert.Equal(t, int64(1), rows[0].TtftLe1000Ms)
	assert.Equal(t, int64(1), rows[0].TtftLe2000Ms)
}

func TestUpsertChannelMetricKeepsDistinctDimensionsInSameBucket(t *testing.T) {
	const channelId = 99003
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id = ?", channelId).Delete(&ChannelMetric{}).Error)
	})
	base := ChannelMetric{
		ChannelId:        channelId,
		Group:            "default",
		ModelName:        "gpt-a",
		Endpoint:         "/v1/responses",
		BucketTs:         1_700_000_000,
		BucketSeconds:    ChannelMetricBucketMinute,
		HistogramVersion: 1,
		AttemptCount:     1,
	}
	require.NoError(t, UpsertChannelMetric(&base))
	base.Id = 0
	base.DimensionsHash = ""
	base.ModelName = "gpt-b"
	require.NoError(t, UpsertChannelMetric(&base))

	rows, err := GetChannelMetricRows(ChannelMetricFilter{ChannelId: channelId}, ChannelMetricBucketMinute, base.BucketTs, base.BucketTs)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.NotEqual(t, rows[0].DimensionsHash, rows[1].DimensionsHash)
}

func TestChannelMetricCrossDatabaseMigrationAndUpsert(t *testing.T) {
	tests := []struct {
		name      string
		envName   string
		dialector func(string) gorm.Dialector
		channelId int
	}{
		{name: "mysql", envName: "TEST_MYSQL_DSN", dialector: func(dsn string) gorm.Dialector { return mysql.Open(dsn) }, channelId: 99011},
		{name: "postgres", envName: "TEST_POSTGRES_DSN", dialector: func(dsn string) gorm.Dialector { return postgres.Open(dsn) }, channelId: 99012},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := os.Getenv(test.envName)
			if dsn == "" {
				t.Skipf("set %s to run channel metric database compatibility test", test.envName)
			}
			db, err := gorm.Open(test.dialector(dsn), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&ChannelMetric{}, &ChannelStatusEvent{}))
			require.NoError(t, db.Where("channel_id = ?", test.channelId).Delete(&ChannelMetric{}).Error)
			require.NoError(t, db.Where("channel_id = ?", test.channelId).Delete(&ChannelStatusEvent{}).Error)
			t.Cleanup(func() {
				require.NoError(t, db.Where("channel_id = ?", test.channelId).Delete(&ChannelMetric{}).Error)
				require.NoError(t, db.Where("channel_id = ?", test.channelId).Delete(&ChannelStatusEvent{}).Error)
			})

			metric := &ChannelMetric{
				ChannelId:        test.channelId,
				Group:            "default",
				ModelName:        "gpt-test",
				Endpoint:         "/v1/responses",
				BucketTs:         1_700_000_000,
				BucketSeconds:    ChannelMetricBucketMinute,
				HistogramVersion: 1,
				AttemptCount:     1,
				PeakConcurrency:  2,
				ErrorCode:        "bad_response_status_code",
				StatusCode:       502,
			}
			require.NoError(t, UpsertChannelMetricWithDB(db, metric))
			metric.Id = 0
			metric.AttemptCount = 2
			metric.PeakConcurrency = 4
			require.NoError(t, UpsertChannelMetricWithDB(db, metric))

			var stored ChannelMetric
			require.NoError(t, db.Where("channel_id = ?", test.channelId).First(&stored).Error)
			assert.Equal(t, int64(3), stored.AttemptCount)
			assert.Equal(t, int64(4), stored.PeakConcurrency)
			assert.Len(t, stored.DimensionsHash, 64)
			groups, err := listChannelMetricDimensionValuesWithDB(
				db,
				ChannelMetricFilter{ChannelId: test.channelId, Group: "default"},
				ChannelMetricBucketMinute,
				metric.BucketTs,
				metric.BucketTs,
				"group",
			)
			require.NoError(t, err)
			assert.Equal(t, []string{"default"}, groups)
			require.NoError(t, db.Create(&ChannelStatusEvent{
				ChannelId:    test.channelId,
				Scope:        "channel",
				FromStatus:   1,
				ToStatus:     3,
				Source:       "database_test",
				ReasonCode:   "upstream_http_500",
				ReasonDetail: "Upstream HTTP 500",
				CreatedAt:    1_700_000_000,
			}).Error)
		})
	}
}

func TestReplaceChannelMetricHourIsIdempotent(t *testing.T) {
	const (
		channelId = 99002
		bucketTs  = int64(1_700_002_800)
	)
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id = ?", channelId).Delete(&ChannelMetric{}).Error)
	})
	metric := ChannelMetric{
		ChannelId:        channelId,
		Group:            "default",
		ModelName:        "gpt-test",
		Endpoint:         "/v1/chat/completions",
		BucketTs:         bucketTs,
		BucketSeconds:    ChannelMetricBucketHour,
		HistogramVersion: 1,
		AttemptCount:     5,
	}

	require.NoError(t, ReplaceChannelMetricHour(bucketTs, []ChannelMetric{metric}))
	require.NoError(t, ReplaceChannelMetricHour(bucketTs, []ChannelMetric{metric}))
	rows, err := GetChannelMetricRows(ChannelMetricFilter{ChannelId: channelId}, ChannelMetricBucketHour, bucketTs, bucketTs)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(5), rows[0].AttemptCount)
}
