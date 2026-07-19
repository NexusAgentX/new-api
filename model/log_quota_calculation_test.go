package model

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaCalculationFromOtherRequiresCompleteFiniteValues(t *testing.T) {
	before, after := quotaCalculationFromOther(map[string]interface{}{
		"quota_before_group":          249.0,
		"quota_after_group_unrounded": 0.0402794103,
	})
	require.NotNil(t, before)
	require.NotNil(t, after)
	assert.Equal(t, 249.0, *before)
	assert.Equal(t, 0.0402794103, *after)

	before, after = quotaCalculationFromOther(map[string]interface{}{
		"quota_before_group":          math.NaN(),
		"quota_after_group_unrounded": 1.0,
	})
	assert.Nil(t, before)
	assert.Nil(t, after)

	before, after = quotaCalculationFromOther(map[string]interface{}{
		"quota_before_group": 249.0,
	})
	assert.Nil(t, before)
	assert.Nil(t, after)
}

func TestRecordConsumeLogPersistsAggregatableQuotaCalculations(t *testing.T) {
	const username = "quota-calculation-stat-user"
	require.NoError(t, LOG_DB.Where("username = ?", username).Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("username = ?", username).Delete(&Log{}).Error)
	})

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", username)
	RecordConsumeLog(ctx, 987654, RecordConsumeLogParams{
		ModelName: "grok-4.5",
		Quota:     1,
		Group:     "grok",
		Other: map[string]interface{}{
			"quota_before_group":          249.1234567891234,
			"quota_after_group_unrounded": 0.0402794103,
		},
	})

	require.NoError(t, createLog(&Log{
		UserId:    987654,
		Username:  username,
		CreatedAt: 1,
		Type:      LogTypeConsume,
		ModelName: "legacy-model",
		Quota:     2,
	}))

	var recorded Log
	require.NoError(t, LOG_DB.Where("username = ? AND model_name = ?", username, "grok-4.5").First(&recorded).Error)
	require.NotNil(t, recorded.QuotaBeforeGroup)
	require.NotNil(t, recorded.QuotaAfterGroupUnrounded)
	assert.InDelta(t, 249.123456789123, *recorded.QuotaBeforeGroup, 1e-12)
	assert.InDelta(t, 0.0402794103, *recorded.QuotaAfterGroupUnrounded, 1e-12)

	stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", username, "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 3, stat.Quota)
	assert.InDelta(t, 249.123456789123, stat.QuotaBeforeGroup, 1e-12)
	assert.InDelta(t, 0.0402794103, stat.QuotaAfterGroupUnrounded, 1e-12)
	assert.EqualValues(t, 1, stat.QuotaCalculationCount)
	assert.EqualValues(t, 2, stat.ConsumeCount)
}
