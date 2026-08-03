package model

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaCalculationPointersRequireAvailableFiniteValues(t *testing.T) {
	before, after := quotaCalculationPointers(true, 249.0, 0.0402794103)
	require.NotNil(t, before)
	require.NotNil(t, after)
	assert.Equal(t, 249.0, *before)
	assert.Equal(t, 0.0402794103, *after)

	before, after = quotaCalculationPointers(true, math.NaN(), 1.0)
	assert.Nil(t, before)
	assert.Nil(t, after)

	before, after = quotaCalculationPointers(false, 249.0, 0.0402794103)
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
		ModelName:                "grok-4.5",
		Quota:                    1,
		QuotaBeforeGroup:         249.1234567891234,
		QuotaAfterGroupUnrounded: 0.0402794103,
		HasQuotaCalculation:      true,
		Group:                    "grok",
		Other:                    map[string]interface{}{"group_ratio": 0.0001617647},
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
	other, err := common.StrToMap(recorded.Other)
	require.NoError(t, err)
	assert.NotContains(t, other, "quota_before_group")
	assert.NotContains(t, other, "quota_after_group_unrounded")

	stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", username, "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 3, stat.Quota)
	assert.InDelta(t, 249.123456789123, stat.QuotaBeforeGroup, 1e-12)
	assert.InDelta(t, 0.0402794103, stat.QuotaAfterGroupUnrounded, 1e-12)
	assert.EqualValues(t, 1, stat.QuotaCalculationCount)
	assert.EqualValues(t, 2, stat.ConsumeCount)
}

func TestSumUsedQuotaExcludesChannelTestsUnlessExplicitlyFiltered(t *testing.T) {
	const username = "channel-test-stat-user"
	require.NoError(t, LOG_DB.Where("username = ?", username).Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("username = ?", username).Delete(&Log{}).Error)
	})

	logs := []Log{
		{
			UserId: 1, Username: username, CreatedAt: 1, Type: LogTypeConsume,
			RequestType: UsageRequestTypeRegular, TokenId: 0, TokenName: "playground", Quota: 10, PromptTokens: 1,
		},
		{
			UserId: 1, Username: username, CreatedAt: 2, Type: LogTypeConsume,
			RequestType: UsageRequestTypeChannelTest, TokenId: 99, TokenName: "模型测试", Quota: 100, PromptTokens: 2,
		},
		{
			UserId: 1, Username: username, CreatedAt: 3, Type: LogTypeConsume,
			TokenId: 0, TokenName: "模型测试", Quota: 200, PromptTokens: 3,
		},
	}
	for index := range logs {
		require.NoError(t, createLog(&logs[index]))
	}

	stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", username, "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 10, stat.Quota)
	assert.EqualValues(t, 1, stat.ConsumeCount)

	testStat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", username, "模型测试", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 300, testStat.Quota)
	assert.EqualValues(t, 2, testStat.ConsumeCount)
	assert.Equal(t, 1, SumUsedToken(LogTypeConsume, 0, 0, "", username, ""))
	assert.Equal(t, 5, SumUsedToken(LogTypeConsume, 0, 0, "", username, "模型测试"))
}
