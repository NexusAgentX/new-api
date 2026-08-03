package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaDataAggregatesExcludeStructuredAndLegacyChannelTests(t *testing.T) {
	truncateTables(t)
	rows := []QuotaData{
		{
			UserID: 1, Username: "alice", ModelName: "model-a", CreatedAt: 3600,
			UseGroup: "vip", TokenID: 11, TokenUsed: 100, Quota: 200, Count: 1,
		},
		{
			UserID: 1, Username: "alice", ModelName: "model-a", CreatedAt: 3600,
			UseGroup: "vip", TokenID: 0, RequestType: UsageRequestTypeRegular,
			TokenUsed: 50, Quota: 70, Count: 2,
		},
		{
			UserID: 1, Username: "alice", ModelName: "model-a", CreatedAt: 3600,
			UseGroup: "vip", TokenID: 99, RequestType: UsageRequestTypeChannelTest,
			TokenUsed: 1_000, Quota: 2_000, Count: 3,
		},
		{
			UserID: 1, Username: "alice", ModelName: "model-a", CreatedAt: 3600,
			UseGroup: "vip", TokenID: 0, TokenUsed: 2_000, Quota: 4_000, Count: 4,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	totals, err := GetRankingQuotaTotals(0, 7200)
	require.NoError(t, err)
	require.Len(t, totals, 1)
	assert.Equal(t, int64(150), totals[0].TotalTokens)

	buckets, err := GetRankingQuotaBuckets(0, 7200, 3600)
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	assert.Equal(t, int64(150), buckets[0].Tokens)

	userTotals, err := GetUserRankingTotals(0, 7200, "quota", 10)
	require.NoError(t, err)
	require.Len(t, userTotals, 1)
	assert.Equal(t, int64(150), userTotals[0].Tokens)
	assert.Equal(t, int64(270), userTotals[0].Quota)
	assert.Equal(t, int64(3), userTotals[0].Count)

	modelTotals, err := GetUserRankingModelTotals(0, 7200, []int{1})
	require.NoError(t, err)
	require.Len(t, modelTotals, 1)
	assert.Equal(t, int64(150), modelTotals[0].Tokens)
	assert.Equal(t, int64(270), modelTotals[0].Quota)
	assert.Equal(t, int64(3), modelTotals[0].Count)

	byUsername, err := GetQuotaDataByUsername("alice", 0, 7200)
	require.NoError(t, err)
	require.Len(t, byUsername, 1)
	assert.Equal(t, 150, byUsername[0].TokenUsed)
	assert.Equal(t, 270, byUsername[0].Quota)
	assert.Equal(t, 3, byUsername[0].Count)

	byUserID, err := GetQuotaDataByUserId(1, 0, 7200)
	require.NoError(t, err)
	require.Len(t, byUserID, 1)
	assert.Equal(t, 150, byUserID[0].TokenUsed)

	byUser, err := GetQuotaDataGroupByUser(0, 7200)
	require.NoError(t, err)
	require.Len(t, byUser, 1)
	assert.Equal(t, 150, byUser[0].TokenUsed)

	allDates, err := GetAllQuotaDates(0, 7200, "")
	require.NoError(t, err)
	require.Len(t, allDates, 1)
	assert.Equal(t, 150, allDates[0].TokenUsed)

	flow, err := GetFlowQuotaData(0, 7200, "", 0, common.RoleRootUser)
	require.NoError(t, err)
	require.Len(t, flow, 2)
	assert.Equal(t, 150, flow[0].TokenUsed+flow[1].TokenUsed)
	assert.Equal(t, 270, flow[0].Quota+flow[1].Quota)
}
