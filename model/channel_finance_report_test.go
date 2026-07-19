package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelFinancePeriodStartUsesNaturalWeekAndMonth(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	value := time.Date(2030, 1, 9, 16, 30, 0, 0, location)

	assert.Equal(t,
		time.Date(2030, 1, 7, 0, 0, 0, 0, location),
		channelFinancePeriodStart(value, ChannelFinanceGranularityWeek),
	)
	assert.Equal(t,
		time.Date(2030, 1, 1, 0, 0, 0, 0, location),
		channelFinancePeriodStart(value, ChannelFinanceGranularityMonth),
	)
}

func TestChannelFinanceReportCombinesFrozenUsageCostWithCurrentFixedCost(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, location)
	end := time.Date(2030, 1, 2, 23, 59, 59, 0, location)
	usageChannelID := 920001
	fixedChannelID := 920002

	require.NoError(t, DB.Create(&Channel{
		Id:             usageChannelID,
		Name:           "metered",
		CostMode:       constant.ChannelCostModeUsageRatio,
		UsageCostRatio: 0.2,
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id:                fixedChannelID,
		Name:              "subscription",
		CostMode:          constant.ChannelCostModeFixedDaily,
		FixedDailyCostUSD: 10,
	}).Error)
	t.Cleanup(func() {
		LOG_DB.Where("channel_id IN ?", []int{usageChannelID, fixedChannelID}).Delete(&Log{})
		DB.Delete(&Channel{}, []int{usageChannelID, fixedChannelID})
	})

	revenueFive := 5.0
	costTwo := 2.0
	revenueMinusOne := -1.0
	costMinusPointFour := -0.4
	revenueThree := 3.0
	require.NoError(t, createLog(&Log{
		CreatedAt:         start.Add(10 * time.Hour).Unix(),
		Type:              LogTypeConsume,
		ChannelId:         usageChannelID,
		ChannelRevenueUSD: &revenueFive,
		ChannelCostUSD:    &costTwo,
		ChannelCostMode:   constant.ChannelCostModeUsageRatio,
	}))
	require.NoError(t, createLog(&Log{
		CreatedAt:         start.Add(12 * time.Hour).Unix(),
		Type:              LogTypeRefund,
		ChannelId:         usageChannelID,
		ChannelRevenueUSD: &revenueMinusOne,
		ChannelCostUSD:    &costMinusPointFour,
		ChannelCostMode:   constant.ChannelCostModeUsageRatio,
	}))
	require.NoError(t, createLog(&Log{
		CreatedAt:         start.AddDate(0, 0, 1).Add(8 * time.Hour).Unix(),
		Type:              LogTypeConsume,
		ChannelId:         fixedChannelID,
		ChannelRevenueUSD: &revenueThree,
		ChannelCostMode:   constant.ChannelCostModeFixedDaily,
	}))

	report, err := GetChannelFinanceReport(start.Unix(), end.Unix(), ChannelFinanceGranularityDay, location)
	require.NoError(t, err)
	assert.InDelta(t, 7, report.Summary.RevenueUSD, 1e-12)
	assert.InDelta(t, 1.6, report.Summary.VariableCostUSD, 1e-12)
	assert.InDelta(t, 20, report.Summary.FixedCostUSD, 1e-12)
	assert.InDelta(t, 21.6, report.Summary.CostUSD, 1e-12)
	assert.InDelta(t, -14.6, report.Summary.ProfitUSD, 1e-12)
	assert.EqualValues(t, 2, report.Summary.RequestCount)
	require.Len(t, report.Periods, 2)
	require.Len(t, report.Channels, 2)
	for _, channel := range report.Channels {
		require.Len(t, channel.Periods, 2)
	}

	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", fixedChannelID).Update("fixed_daily_cost_usd", 15).Error)
	recalculated, err := GetChannelFinanceReport(start.Unix(), end.Unix(), ChannelFinanceGranularityDay, location)
	require.NoError(t, err)
	assert.InDelta(t, 30, recalculated.Summary.FixedCostUSD, 1e-12)
	assert.InDelta(t, 1.6, recalculated.Summary.VariableCostUSD, 1e-12)
}

func TestChannelFinanceReportMarksUnavailableRequestFinanceData(t *testing.T) {
	location := time.UTC
	start := time.Date(2031, 3, 1, 0, 0, 0, 0, location)
	channelID := 920003
	require.NoError(t, DB.Create(&Channel{
		Id:             channelID,
		Name:           "legacy-metered",
		CostMode:       constant.ChannelCostModeUsageRatio,
		UsageCostRatio: 0.5,
	}).Error)
	t.Cleanup(func() {
		LOG_DB.Where("channel_id = ?", channelID).Delete(&Log{})
		DB.Delete(&Channel{}, channelID)
	})
	require.NoError(t, createLog(&Log{
		CreatedAt:       start.Add(time.Hour).Unix(),
		Type:            LogTypeConsume,
		ChannelId:       channelID,
		ChannelCostMode: constant.ChannelCostModeUsageRatio,
	}))
	require.NoError(t, createLog(&Log{
		CreatedAt:       start.Add(2 * time.Hour).Unix(),
		Type:            LogTypeRefund,
		ChannelId:       channelID,
		ChannelCostMode: constant.ChannelCostModeUsageRatio,
	}))

	report, err := GetChannelFinanceReport(start.Unix(), start.Add(24*time.Hour-time.Second).Unix(), ChannelFinanceGranularityMonth, location)
	require.NoError(t, err)
	assert.EqualValues(t, 1, report.Summary.RequestCount)
	assert.EqualValues(t, 2, report.Summary.MissingRevenueCount)
	assert.EqualValues(t, 2, report.Summary.MissingCostCount)
	require.Len(t, report.Periods, 1)
	assert.Equal(t, start.Unix(), report.Periods[0].PeriodStart)
}
