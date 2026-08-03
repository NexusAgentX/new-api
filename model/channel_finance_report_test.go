package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"

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

func TestChannelFinanceReportCombinesChannelInfrastructureAndCheckinCosts(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, location)
	end := time.Date(2030, 1, 2, 23, 59, 59, 0, location)
	usageChannelID := 920001
	fixedChannelID := 920002
	checkinUserIDs := []int{930001, 930002}
	originalQuotaPerUnit := common.QuotaPerUnit
	originalInfrastructureCost := operation_setting.GetFinanceSetting().InfrastructureDailyCostUSD
	common.QuotaPerUnit = 500_000
	operation_setting.GetFinanceSetting().InfrastructureDailyCostUSD = 3

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
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetFinanceSetting().InfrastructureDailyCostUSD = originalInfrastructureCost
		LOG_DB.Where("channel_id IN ?", []int{usageChannelID, fixedChannelID}).Delete(&Log{})
		DB.Where("user_id IN ?", checkinUserIDs).Delete(&Checkin{})
		DB.Delete(&Channel{}, []int{usageChannelID, fixedChannelID})
	})
	require.NoError(t, DB.Create(&[]Checkin{
		{
			UserId:       checkinUserIDs[0],
			CheckinDate:  "2030-01-01",
			QuotaAwarded: 500_000,
			CreatedAt:    start.Add(9 * time.Hour).Unix(),
		},
		{
			UserId:       checkinUserIDs[1],
			CheckinDate:  "2030-01-02",
			QuotaAwarded: 250_000,
			CreatedAt:    start.AddDate(0, 0, 1).Add(9 * time.Hour).Unix(),
		},
	}).Error)

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
	assert.InDelta(t, 21.6, report.Summary.ChannelCostUSD, 1e-12)
	assert.InDelta(t, 6, report.Summary.InfrastructureCostUSD, 1e-12)
	assert.InDelta(t, 1.5, report.Summary.CheckinCostUSD, 1e-12)
	assert.InDelta(t, 29.1, report.Summary.CostUSD, 1e-12)
	assert.InDelta(t, -22.1, report.Summary.ProfitUSD, 1e-12)
	assert.EqualValues(t, 2, report.Summary.RequestCount)
	require.Len(t, report.Periods, 2)
	assert.InDelta(t, 3, report.Periods[0].InfrastructureCostUSD, 1e-12)
	assert.InDelta(t, 1, report.Periods[0].CheckinCostUSD, 1e-12)
	assert.InDelta(t, 0.5, report.Periods[1].CheckinCostUSD, 1e-12)
	require.Len(t, report.Channels, 2)
	for _, channel := range report.Channels {
		assert.False(t, channel.Deleted)
		assert.Zero(t, channel.InfrastructureCostUSD)
		assert.Zero(t, channel.CheckinCostUSD)
		assert.InDelta(t, channel.ChannelCostUSD, channel.CostUSD, 1e-12)
		require.Len(t, channel.Periods, 2)
	}

	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", fixedChannelID).Update("fixed_daily_cost_usd", 15).Error)
	recalculated, err := GetChannelFinanceReport(start.Unix(), end.Unix(), ChannelFinanceGranularityDay, location)
	require.NoError(t, err)
	assert.InDelta(t, 30, recalculated.Summary.FixedCostUSD, 1e-12)
	assert.InDelta(t, 1.6, recalculated.Summary.VariableCostUSD, 1e-12)
	assert.InDelta(t, 31.6, recalculated.Summary.ChannelCostUSD, 1e-12)
	assert.InDelta(t, 39.1, recalculated.Summary.CostUSD, 1e-12)
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

func TestChannelFinanceReportMarksDeletedChannels(t *testing.T) {
	start := time.Date(2032, 4, 5, 0, 0, 0, 0, time.UTC)
	channelID := 920004
	revenue := 4.0
	cost := 1.0

	require.NoError(t, DB.Create(&Channel{
		Id:             channelID,
		Name:           "temporary-metered",
		CostMode:       constant.ChannelCostModeUsageRatio,
		UsageCostRatio: 0.25,
	}).Error)
	t.Cleanup(func() {
		LOG_DB.Where("channel_id = ?", channelID).Delete(&Log{})
		DB.Delete(&Channel{}, channelID)
	})
	require.NoError(t, createLog(&Log{
		CreatedAt:         start.Add(time.Hour).Unix(),
		Type:              LogTypeConsume,
		ChannelId:         channelID,
		ChannelRevenueUSD: &revenue,
		ChannelCostUSD:    &cost,
		ChannelCostMode:   constant.ChannelCostModeUsageRatio,
	}))
	require.NoError(t, DB.Delete(&Channel{}, channelID).Error)

	report, err := GetChannelFinanceReport(start.Unix(), start.Add(24*time.Hour-time.Second).Unix(), ChannelFinanceGranularityDay, time.UTC)
	require.NoError(t, err)
	require.Len(t, report.Channels, 1)
	assert.Equal(t, channelID, report.Channels[0].ChannelID)
	assert.Equal(t, "#920004", report.Channels[0].ChannelName)
	assert.True(t, report.Channels[0].Deleted)
	assert.InDelta(t, revenue, report.Channels[0].RevenueUSD, 1e-12)
	assert.InDelta(t, cost, report.Channels[0].CostUSD, 1e-12)
}

func TestChannelFinanceReportSeparatesChannelTestCostFromCustomerRevenue(t *testing.T) {
	truncateTables(t)
	start := time.Date(2033, 5, 6, 0, 0, 0, 0, time.UTC)
	usageChannelID := 920005
	unconfiguredChannelID := 920006
	require.NoError(t, DB.Create(&[]Channel{
		{
			Id: usageChannelID, Name: "metered-tests",
			CostMode: constant.ChannelCostModeUsageRatio, UsageCostRatio: 0.08,
		},
		{
			Id: unconfiguredChannelID, Name: "missing-test-cost",
			CostMode: constant.ChannelCostModeNone,
		},
	}).Error)

	revenueFive := 5.0
	revenueOne := 1.0
	legacyInflatedRevenue := 7.0
	structuredInflatedRevenue := 9.0
	costTwo := 2.0
	costPointOne := 0.1
	legacyTestCost := 0.25
	structuredTestCost := 0.5
	logs := []Log{
		{
			CreatedAt: start.Add(time.Hour).Unix(), Type: LogTypeConsume, ChannelId: usageChannelID,
			TokenId: 11, ChannelRevenueUSD: &revenueFive, ChannelCostUSD: &costTwo,
			ChannelCostMode: constant.ChannelCostModeUsageRatio,
		},
		{
			CreatedAt: start.Add(2 * time.Hour).Unix(), Type: LogTypeConsume, ChannelId: usageChannelID,
			RequestType: UsageRequestTypeRegular, TokenId: 0,
			ChannelRevenueUSD: &revenueOne, ChannelCostUSD: &costPointOne,
			ChannelCostMode: constant.ChannelCostModeUsageRatio,
		},
		{
			CreatedAt: start.Add(3 * time.Hour).Unix(), Type: LogTypeConsume, ChannelId: usageChannelID,
			RequestType: UsageRequestTypeChannelTest, TokenId: 99,
			ChannelRevenueUSD: &structuredInflatedRevenue, ChannelCostUSD: &structuredTestCost,
			ChannelCostMode: constant.ChannelCostModeUsageRatio,
		},
		{
			CreatedAt: start.Add(4 * time.Hour).Unix(), Type: LogTypeConsume, ChannelId: usageChannelID,
			TokenId: 0, TokenName: "模型测试",
			ChannelRevenueUSD: &legacyInflatedRevenue, ChannelCostUSD: &legacyTestCost,
			ChannelCostMode: constant.ChannelCostModeUsageRatio,
		},
		{
			CreatedAt: start.Add(5 * time.Hour).Unix(), Type: LogTypeConsume, ChannelId: unconfiguredChannelID,
			RequestType: UsageRequestTypeChannelTest, TokenId: 0,
			ChannelCostMode: constant.ChannelCostModeNone,
		},
	}
	for index := range logs {
		require.NoError(t, createLog(&logs[index]))
	}

	report, err := GetChannelFinanceReport(
		start.Unix(),
		start.Add(24*time.Hour-time.Second).Unix(),
		ChannelFinanceGranularityDay,
		time.UTC,
	)
	require.NoError(t, err)
	assert.InDelta(t, 6, report.Summary.RevenueUSD, 1e-12)
	assert.InDelta(t, 2.85, report.Summary.VariableCostUSD, 1e-12)
	assert.InDelta(t, 0.75, report.Summary.TestCostUSD, 1e-12)
	assert.EqualValues(t, 2, report.Summary.RequestCount)
	assert.EqualValues(t, 3, report.Summary.TestRequestCount)
	assert.Zero(t, report.Summary.MissingRevenueCount)
	assert.EqualValues(t, 1, report.Summary.MissingCostCount)
	assert.EqualValues(t, 1, report.Summary.MissingTestCostCount)
}
