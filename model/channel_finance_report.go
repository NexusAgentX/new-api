package model

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

const (
	ChannelFinanceGranularityDay   = "day"
	ChannelFinanceGranularityWeek  = "week"
	ChannelFinanceGranularityMonth = "month"
)

type ChannelFinanceSummary struct {
	RevenueUSD            float64 `json:"revenue_usd"`
	VariableCostUSD       float64 `json:"variable_cost_usd"`
	FixedCostUSD          float64 `json:"fixed_cost_usd"`
	ChannelCostUSD        float64 `json:"channel_cost_usd"`
	InfrastructureCostUSD float64 `json:"infrastructure_cost_usd"`
	CheckinCostUSD        float64 `json:"checkin_cost_usd"`
	CostUSD               float64 `json:"cost_usd"`
	ProfitUSD             float64 `json:"profit_usd"`
	Margin                float64 `json:"margin"`
	RequestCount          int64   `json:"request_count"`
	MissingRevenueCount   int64   `json:"missing_revenue_count"`
	MissingCostCount      int64   `json:"missing_cost_count"`
}

type ChannelFinancePeriod struct {
	PeriodStart int64 `json:"period_start"`
	ChannelFinanceSummary
}

type ChannelFinanceChannel struct {
	ChannelID   int                    `json:"channel_id"`
	ChannelName string                 `json:"channel_name"`
	Deleted     bool                   `json:"deleted"`
	CostMode    string                 `json:"cost_mode"`
	CostSetting float64                `json:"cost_setting"`
	Periods     []ChannelFinancePeriod `json:"periods"`
	ChannelFinanceSummary
}

type ChannelFinanceReport struct {
	Summary  ChannelFinanceSummary   `json:"summary"`
	Periods  []ChannelFinancePeriod  `json:"periods"`
	Channels []ChannelFinanceChannel `json:"channels"`
}

type channelFinanceAccumulator struct {
	revenueUSD            decimal.Decimal
	variableCostUSD       decimal.Decimal
	fixedCostUSD          decimal.Decimal
	infrastructureCostUSD decimal.Decimal
	checkinCostUSD        decimal.Decimal
	requestCount          int64
	missingRevenue        int64
	missingCost           int64
}

type channelFinanceLogRow struct {
	CreatedAt         int64    `gorm:"column:created_at"`
	Type              int      `gorm:"column:type"`
	ChannelID         int      `gorm:"column:channel_id"`
	ChannelRevenueUSD *float64 `gorm:"column:channel_revenue_usd"`
	ChannelCostUSD    *float64 `gorm:"column:channel_cost_usd"`
	ChannelCostMode   string   `gorm:"column:channel_cost_mode"`
}

type checkinFinanceRow struct {
	CreatedAt    int64 `gorm:"column:created_at"`
	QuotaAwarded int   `gorm:"column:quota_awarded"`
}

func GetChannelFinanceReport(startTimestamp, endTimestamp int64, granularity string, location *time.Location) (ChannelFinanceReport, error) {
	if startTimestamp > endTimestamp {
		return ChannelFinanceReport{}, fmt.Errorf("start timestamp must not exceed end timestamp")
	}
	if location == nil {
		location = time.UTC
	}
	if granularity != ChannelFinanceGranularityDay && granularity != ChannelFinanceGranularityWeek && granularity != ChannelFinanceGranularityMonth {
		return ChannelFinanceReport{}, fmt.Errorf("invalid granularity: %s", granularity)
	}

	channels, err := loadChannelFinanceConfigs()
	if err != nil {
		return ChannelFinanceReport{}, err
	}
	channelIndexByID := make(map[int]int, len(channels))
	channelTotals := make(map[int]*channelFinanceAccumulator, len(channels))
	channelPeriodTotals := make(map[int]map[int64]*channelFinanceAccumulator, len(channels))
	for index := range channels {
		channelIndexByID[channels[index].ChannelID] = index
		channelTotals[channels[index].ChannelID] = &channelFinanceAccumulator{}
		channelPeriodTotals[channels[index].ChannelID] = make(map[int64]*channelFinanceAccumulator)
	}
	periodTotals := make(map[int64]*channelFinanceAccumulator)

	rows := make([]channelFinanceLogRow, 0)
	err = LOG_DB.Model(&Log{}).
		Select("created_at, type, channel_id, channel_revenue_usd, channel_cost_usd, channel_cost_mode").
		Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp).
		Where("type IN ?", []int{LogTypeConsume, LogTypeRefund}).
		Where("token_name <> ?", "模型测试").
		Find(&rows).Error
	if err != nil {
		return ChannelFinanceReport{}, err
	}

	for _, row := range rows {
		channelIndex, ok := channelIndexByID[row.ChannelID]
		if !ok {
			costMode := row.ChannelCostMode
			if costMode == "" {
				costMode = constant.ChannelCostModeNone
			}
			channels = append(channels, ChannelFinanceChannel{
				ChannelID:   row.ChannelID,
				ChannelName: fmt.Sprintf("#%d", row.ChannelID),
				Deleted:     true,
				CostMode:    costMode,
			})
			channelIndex = len(channels) - 1
			channelIndexByID[row.ChannelID] = channelIndex
			channelTotals[row.ChannelID] = &channelFinanceAccumulator{}
			channelPeriodTotals[row.ChannelID] = make(map[int64]*channelFinanceAccumulator)
		}
		channel := &channels[channelIndex]
		channelTotal := channelTotals[row.ChannelID]
		periodStart := channelFinancePeriodStart(time.Unix(row.CreatedAt, 0).In(location), granularity).Unix()
		periodTotal := accumulatorForPeriod(periodTotals, periodStart)
		channelPeriodTotal := accumulatorForPeriod(channelPeriodTotals[row.ChannelID], periodStart)

		if row.Type == LogTypeConsume {
			channelTotal.requestCount++
			channelPeriodTotal.requestCount++
			periodTotal.requestCount++
		}
		if row.ChannelRevenueUSD == nil {
			channelTotal.missingRevenue++
			channelPeriodTotal.missingRevenue++
			periodTotal.missingRevenue++
		} else {
			value := decimal.NewFromFloat(*row.ChannelRevenueUSD)
			channelTotal.revenueUSD = channelTotal.revenueUSD.Add(value)
			channelPeriodTotal.revenueUSD = channelPeriodTotal.revenueUSD.Add(value)
			periodTotal.revenueUSD = periodTotal.revenueUSD.Add(value)
		}

		if row.ChannelCostUSD == nil {
			usageCostExpected := row.ChannelCostMode == constant.ChannelCostModeUsageRatio ||
				(row.ChannelCostMode == "" && channel.CostMode == constant.ChannelCostModeUsageRatio)
			if usageCostExpected {
				channelTotal.missingCost++
				channelPeriodTotal.missingCost++
				periodTotal.missingCost++
			}
			continue
		}
		value := decimal.NewFromFloat(*row.ChannelCostUSD)
		channelTotal.variableCostUSD = channelTotal.variableCostUSD.Add(value)
		channelPeriodTotal.variableCostUSD = channelPeriodTotal.variableCostUSD.Add(value)
		periodTotal.variableCostUSD = periodTotal.variableCostUSD.Add(value)
	}

	checkinRows := make([]checkinFinanceRow, 0)
	if err := DB.Model(&Checkin{}).
		Select("created_at, quota_awarded").
		Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp).
		Find(&checkinRows).Error; err != nil {
		return ChannelFinanceReport{}, err
	}
	if len(checkinRows) > 0 && (common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0)) {
		return ChannelFinanceReport{}, fmt.Errorf("quota per unit must be greater than zero")
	}
	for _, row := range checkinRows {
		if row.QuotaAwarded <= 0 {
			continue
		}
		periodStart := channelFinancePeriodStart(time.Unix(row.CreatedAt, 0).In(location), granularity).Unix()
		periodTotal := accumulatorForPeriod(periodTotals, periodStart)
		cost := decimal.NewFromInt(int64(row.QuotaAwarded)).Div(decimal.NewFromFloat(common.QuotaPerUnit))
		periodTotal.checkinCostUSD = periodTotal.checkinCostUSD.Add(cost)
	}

	startDay := channelFinanceDayStart(time.Unix(startTimestamp, 0).In(location))
	endDay := channelFinanceDayStart(time.Unix(endTimestamp, 0).In(location))
	infrastructureDailyCostUSD := operation_setting.GetFinanceSetting().InfrastructureDailyCostUSD
	if err := operation_setting.ValidateInfrastructureDailyCostUSD(infrastructureDailyCostUSD); err != nil {
		return ChannelFinanceReport{}, err
	}
	for day := startDay; !day.After(endDay); day = day.AddDate(0, 0, 1) {
		periodStart := channelFinancePeriodStart(day, granularity).Unix()
		periodTotal := accumulatorForPeriod(periodTotals, periodStart)
		periodTotal.infrastructureCostUSD = periodTotal.infrastructureCostUSD.Add(decimal.NewFromFloat(infrastructureDailyCostUSD))
		for index := range channels {
			channel := &channels[index]
			if channel.CostMode != constant.ChannelCostModeFixedDaily {
				continue
			}
			value := decimal.NewFromFloat(channel.CostSetting)
			channelTotals[channel.ChannelID].fixedCostUSD = channelTotals[channel.ChannelID].fixedCostUSD.Add(value)
			channelPeriodTotal := accumulatorForPeriod(channelPeriodTotals[channel.ChannelID], periodStart)
			channelPeriodTotal.fixedCostUSD = channelPeriodTotal.fixedCostUSD.Add(value)
			periodTotal.fixedCostUSD = periodTotal.fixedCostUSD.Add(value)
		}
	}

	periodKeys := make([]int64, 0, len(periodTotals))
	for key := range periodTotals {
		periodKeys = append(periodKeys, key)
	}
	sort.Slice(periodKeys, func(i, j int) bool { return periodKeys[i] < periodKeys[j] })
	report := ChannelFinanceReport{
		Periods:  make([]ChannelFinancePeriod, 0, len(periodKeys)),
		Channels: channels,
	}
	grandTotal := &channelFinanceAccumulator{}
	for _, key := range periodKeys {
		accumulator := periodTotals[key]
		report.Periods = append(report.Periods, ChannelFinancePeriod{
			PeriodStart:           key,
			ChannelFinanceSummary: accumulator.summary(),
		})
		grandTotal.add(accumulator)
	}
	report.Summary = grandTotal.summary()
	for index := range report.Channels {
		channelID := report.Channels[index].ChannelID
		report.Channels[index].ChannelFinanceSummary = channelTotals[channelID].summary()
		report.Channels[index].Periods = make([]ChannelFinancePeriod, 0, len(periodKeys))
		for _, key := range periodKeys {
			accumulator := channelPeriodTotals[channelID][key]
			if accumulator == nil {
				accumulator = &channelFinanceAccumulator{}
			}
			report.Channels[index].Periods = append(report.Channels[index].Periods, ChannelFinancePeriod{
				PeriodStart:           key,
				ChannelFinanceSummary: accumulator.summary(),
			})
		}
	}
	sort.Slice(report.Channels, func(i, j int) bool {
		if report.Channels[i].ProfitUSD == report.Channels[j].ProfitUSD {
			return report.Channels[i].ChannelID < report.Channels[j].ChannelID
		}
		return report.Channels[i].ProfitUSD > report.Channels[j].ProfitUSD
	})
	return report, nil
}

func loadChannelFinanceConfigs() ([]ChannelFinanceChannel, error) {
	var storedChannels []Channel
	if err := DB.Select("id, name, cost_mode, fixed_daily_cost_usd, usage_cost_ratio").Find(&storedChannels).Error; err != nil {
		return nil, err
	}
	channels := make([]ChannelFinanceChannel, 0, len(storedChannels))
	for _, channel := range storedChannels {
		if err := channel.ValidateCostConfig(); err != nil {
			return nil, fmt.Errorf("invalid cost config for channel %d: %w", channel.Id, err)
		}
		costMode := channel.CostMode
		if costMode == "" {
			costMode = constant.ChannelCostModeNone
		}
		costSetting := 0.0
		if costMode == constant.ChannelCostModeFixedDaily {
			costSetting = channel.FixedDailyCostUSD
		} else if costMode == constant.ChannelCostModeUsageRatio {
			costSetting = channel.UsageCostRatio
		}
		channels = append(channels, ChannelFinanceChannel{
			ChannelID:   channel.Id,
			ChannelName: channel.Name,
			CostMode:    costMode,
			CostSetting: costSetting,
		})
	}
	return channels, nil
}

func channelFinanceDayStart(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}

func channelFinancePeriodStart(value time.Time, granularity string) time.Time {
	value = channelFinanceDayStart(value)
	switch granularity {
	case ChannelFinanceGranularityWeek:
		weekday := (int(value.Weekday()) + 6) % 7
		return value.AddDate(0, 0, -weekday)
	case ChannelFinanceGranularityMonth:
		return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, value.Location())
	default:
		return value
	}
}

func accumulatorForPeriod(periods map[int64]*channelFinanceAccumulator, periodStart int64) *channelFinanceAccumulator {
	accumulator := periods[periodStart]
	if accumulator == nil {
		accumulator = &channelFinanceAccumulator{}
		periods[periodStart] = accumulator
	}
	return accumulator
}

func (accumulator *channelFinanceAccumulator) add(other *channelFinanceAccumulator) {
	accumulator.revenueUSD = accumulator.revenueUSD.Add(other.revenueUSD)
	accumulator.variableCostUSD = accumulator.variableCostUSD.Add(other.variableCostUSD)
	accumulator.fixedCostUSD = accumulator.fixedCostUSD.Add(other.fixedCostUSD)
	accumulator.infrastructureCostUSD = accumulator.infrastructureCostUSD.Add(other.infrastructureCostUSD)
	accumulator.checkinCostUSD = accumulator.checkinCostUSD.Add(other.checkinCostUSD)
	accumulator.requestCount += other.requestCount
	accumulator.missingRevenue += other.missingRevenue
	accumulator.missingCost += other.missingCost
}

func (accumulator *channelFinanceAccumulator) summary() ChannelFinanceSummary {
	channelCost := accumulator.variableCostUSD.Add(accumulator.fixedCostUSD)
	cost := channelCost.Add(accumulator.infrastructureCostUSD).Add(accumulator.checkinCostUSD)
	profit := accumulator.revenueUSD.Sub(cost)
	margin := decimal.Zero
	if !accumulator.revenueUSD.IsZero() {
		margin = profit.Div(accumulator.revenueUSD)
	}
	return ChannelFinanceSummary{
		RevenueUSD:            accumulator.revenueUSD.InexactFloat64(),
		VariableCostUSD:       accumulator.variableCostUSD.InexactFloat64(),
		FixedCostUSD:          accumulator.fixedCostUSD.InexactFloat64(),
		ChannelCostUSD:        channelCost.InexactFloat64(),
		InfrastructureCostUSD: accumulator.infrastructureCostUSD.InexactFloat64(),
		CheckinCostUSD:        accumulator.checkinCostUSD.InexactFloat64(),
		CostUSD:               cost.InexactFloat64(),
		ProfitUSD:             profit.InexactFloat64(),
		Margin:                margin.InexactFloat64(),
		RequestCount:          accumulator.requestCount,
		MissingRevenueCount:   accumulator.missingRevenue,
		MissingCostCount:      accumulator.missingCost,
	}
}
