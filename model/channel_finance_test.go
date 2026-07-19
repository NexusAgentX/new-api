package model

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelCostConfigValidationAndNormalization(t *testing.T) {
	tests := []struct {
		name    string
		channel Channel
		wantErr bool
	}{
		{
			name: "fixed daily cost",
			channel: Channel{
				CostMode:          constant.ChannelCostModeFixedDaily,
				FixedDailyCostUSD: 12.5,
				UsageCostRatio:    0.8,
			},
		},
		{
			name: "zero usage cost",
			channel: Channel{
				CostMode:       constant.ChannelCostModeUsageRatio,
				UsageCostRatio: 0,
			},
		},
		{
			name: "invalid mode",
			channel: Channel{
				CostMode: "monthly",
			},
			wantErr: true,
		},
		{
			name: "non finite cost",
			channel: Channel{
				CostMode:          constant.ChannelCostModeFixedDaily,
				FixedDailyCostUSD: math.Inf(1),
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.channel.ValidateCostConfig()
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			test.channel.NormalizeCostConfig()
			if test.channel.CostMode == constant.ChannelCostModeFixedDaily {
				assert.Zero(t, test.channel.UsageCostRatio)
			}
			if test.channel.CostMode == constant.ChannelCostModeUsageRatio {
				assert.Zero(t, test.channel.FixedDailyCostUSD)
			}
		})
	}
}

func TestChannelUpdatePersistsZeroedInactiveCostFields(t *testing.T) {
	channel := &Channel{
		Name:              "cost-update-test",
		Key:               "test-key",
		CostMode:          constant.ChannelCostModeFixedDaily,
		FixedDailyCostUSD: 12.5,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() { DB.Delete(&Channel{}, channel.Id) })

	channel.CostMode = constant.ChannelCostModeNone
	channel.FixedDailyCostUSD = 0
	require.NoError(t, channel.Update())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, constant.ChannelCostModeNone, stored.CostMode)
	assert.Zero(t, stored.FixedDailyCostUSD)
	assert.Zero(t, stored.UsageCostRatio)
}

func TestCalculateChannelFinanceValuesFreeRevenueStillRecordsUsageCost(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})
	common.QuotaPerUnit = 500_000
	common.MemoryCacheEnabled = false

	channel := &Channel{
		Id:             910001,
		Name:           "usage-finance-test",
		CostMode:       constant.ChannelCostModeUsageRatio,
		UsageCostRatio: 0.4,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() { DB.Delete(&Channel{}, channel.Id) })

	quotaBeforeGroup := 249.0
	values := calculateChannelFinanceValues(LogTypeConsume, channel.Id, 0, &quotaBeforeGroup)

	require.NotNil(t, values.RevenueUSD)
	require.NotNil(t, values.CostUSD)
	require.NotNil(t, values.CostRatio)
	assert.Zero(t, *values.RevenueUSD)
	assert.InDelta(t, 249.0/500_000*0.4, *values.CostUSD, 1e-12)
	assert.Equal(t, 0.4, *values.CostRatio)
	assert.Equal(t, constant.ChannelCostModeUsageRatio, values.CostMode)
}

func TestCalculateChannelFinanceValuesRefundIsSigned(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })
	common.QuotaPerUnit = 500_000

	values := calculateChannelFinanceValues(LogTypeRefund, 0, 250, nil)

	require.NotNil(t, values.RevenueUSD)
	assert.InDelta(t, -0.0005, *values.RevenueUSD, 1e-12)
	assert.Nil(t, values.CostUSD)
}

func TestCalculateChannelFinanceValuesFixedCostIsNotAssignedToRequest(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})
	common.QuotaPerUnit = 500_000
	common.MemoryCacheEnabled = false

	channel := &Channel{
		Id:                910002,
		Name:              "fixed-finance-test",
		CostMode:          constant.ChannelCostModeFixedDaily,
		FixedDailyCostUSD: 12.5,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() { DB.Delete(&Channel{}, channel.Id) })

	quotaBeforeGroup := 1_000.0
	values := calculateChannelFinanceValues(LogTypeConsume, channel.Id, 500, &quotaBeforeGroup)

	require.NotNil(t, values.RevenueUSD)
	assert.InDelta(t, 0.001, *values.RevenueUSD, 1e-12)
	assert.Nil(t, values.CostUSD)
	assert.Nil(t, values.CostRatio)
	assert.Equal(t, constant.ChannelCostModeFixedDaily, values.CostMode)
}

func TestRecordConsumeLogPersistsRequestTimeChannelFinance(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})
	common.QuotaPerUnit = 500_000
	common.MemoryCacheEnabled = false

	channel := &Channel{
		Id:             910003,
		Name:           "persisted-finance-test",
		CostMode:       constant.ChannelCostModeUsageRatio,
		UsageCostRatio: 0.25,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		LOG_DB.Where("channel_id = ?", channel.Id).Delete(&Log{})
		DB.Delete(&Channel{}, channel.Id)
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "finance-log-user")
	RecordConsumeLog(ctx, 910003, RecordConsumeLogParams{
		ChannelId:                channel.Id,
		ModelName:                "finance-model",
		Quota:                    1,
		QuotaBeforeGroup:         249,
		QuotaAfterGroupUnrounded: 0.04,
		HasQuotaCalculation:      true,
		Other:                    map[string]interface{}{},
	})

	var recorded Log
	require.NoError(t, LOG_DB.Where("channel_id = ?", channel.Id).Order("created_at DESC").First(&recorded).Error)
	require.NotNil(t, recorded.ChannelRevenueUSD)
	require.NotNil(t, recorded.ChannelCostUSD)
	require.NotNil(t, recorded.ChannelCostRatio)
	assert.InDelta(t, 1.0/500_000, *recorded.ChannelRevenueUSD, 1e-12)
	assert.InDelta(t, 249.0/500_000*0.25, *recorded.ChannelCostUSD, 1e-12)
	assert.Equal(t, 0.25, *recorded.ChannelCostRatio)
	assert.Equal(t, constant.ChannelCostModeUsageRatio, recorded.ChannelCostMode)
}
