package model

import (
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/shopspring/decimal"
)

type ChannelFinanceValues struct {
	RevenueUSD *float64
	CostUSD    *float64
	CostRatio  *float64
	CostMode   string
}

func calculateChannelFinanceValues(logType, channelID, quota int, quotaBeforeGroup *float64) ChannelFinanceValues {
	values := ChannelFinanceValues{}
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return values
	}

	sign := int64(1)
	switch logType {
	case LogTypeConsume:
	case LogTypeRefund:
		sign = -1
	default:
		return values
	}

	revenue := decimal.NewFromInt(int64(quota)).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Mul(decimal.NewFromInt(sign))
	values.RevenueUSD = financeDecimalPointer(revenue)

	if channelID <= 0 {
		return values
	}
	channel, err := CacheGetChannel(channelID)
	if err != nil {
		return values
	}
	values.CostMode = channel.CostMode
	if values.CostMode == "" {
		values.CostMode = constant.ChannelCostModeNone
	}
	if values.CostMode != constant.ChannelCostModeUsageRatio {
		return values
	}

	ratio := channel.UsageCostRatio
	if ratio < 0 || ratio > constant.MaxChannelUsageCostRatio || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return values
	}
	values.CostRatio = &ratio
	if quotaBeforeGroup == nil {
		return values
	}

	cost := decimal.NewFromFloat(*quotaBeforeGroup).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(ratio)).
		Mul(decimal.NewFromInt(sign))
	values.CostUSD = financeDecimalPointer(cost)
	return values
}

func financeDecimalPointer(value decimal.Decimal) *float64 {
	result, _ := value.Float64()
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return nil
	}
	return &result
}
