package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type ChannelTestBillingResult struct {
	Quota                    int
	QuotaBeforeGroup         float64
	QuotaAfterGroupUnrounded float64
	HasQuotaCalculation      bool
	TieredResult             *billingexpr.TieredResult
}

// CalculateChannelTestBilling applies the normal model billing calculation to
// returned channel-test usage without touching a user wallet. The caller sets
// PriceData.GroupRatioInfo to the selected channel's test-cost policy first.
func CalculateChannelTestBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage) ChannelTestBillingResult {
	if relayInfo == nil || usage == nil {
		return ChannelTestBillingResult{}
	}

	billingUsage := effectiveBillingUsage(usage)
	summary := calculateTextQuotaSummary(ctx, relayInfo, billingUsage)
	if !summary.hasBillableUsage() {
		if !relayInfo.PriceData.UsePrice {
			return ChannelTestBillingResult{HasQuotaCalculation: true}
		}

		quotaBeforeGroup := relayInfo.PriceData.ApplyOtherRatiosToDecimal(
			decimal.NewFromFloat(relayInfo.PriceData.ModelPrice).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		quotaAfterGroup := quotaBeforeGroup.Mul(decimal.NewFromFloat(relayInfo.PriceData.GroupRatioInfo.GroupRatio))
		quota, clamp := common.QuotaFromDecimalChecked(quotaAfterGroup)
		noteQuotaClamp(relayInfo, clamp)
		quota = common.ApplyMinimumBillableQuota(
			quota,
			quotaAfterGroup.IsPositive(),
			relayInfo.PriceData.GroupRatioInfo.AllowZeroQuota,
		)
		return ChannelTestBillingResult{
			Quota:                    quota,
			QuotaBeforeGroup:         quotaBeforeGroup.InexactFloat64(),
			QuotaAfterGroupUnrounded: quotaAfterGroup.InexactFloat64(),
			HasQuotaCalculation:      true,
		}
	}

	if relayInfo.TieredBillingSnapshot != nil {
		usedVars := billingexpr.UsedVars(relayInfo.TieredBillingSnapshot.ExprString)
		isClaudeUsageSemantic := summary.IsClaudeUsageSemantic
		ok, quota, tieredResult := TryTieredSettle(
			relayInfo,
			BuildTieredTokenParams(billingUsage, isClaudeUsageSemantic, usedVars),
		)
		if ok {
			result := ChannelTestBillingResult{
				Quota:        quota,
				TieredResult: tieredResult,
			}
			if tieredResult == nil {
				return result
			}
			result.Quota = composeTieredTextQuota(relayInfo, summary, quota, tieredResult)
			result.QuotaBeforeGroup = decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).
				Add(summary.ToolCallSurchargeQuotaBeforeGroup).
				InexactFloat64()
			result.QuotaAfterGroupUnrounded = decimal.NewFromFloat(tieredResult.ActualQuotaAfterGroupUnrounded).
				Add(summary.ToolCallSurchargeQuota).
				InexactFloat64()
			result.HasQuotaCalculation = true
			return result
		}
	}

	return ChannelTestBillingResult{
		Quota:                    summary.Quota,
		QuotaBeforeGroup:         summary.QuotaBeforeGroup.InexactFloat64(),
		QuotaAfterGroupUnrounded: summary.QuotaAfterGroupUnrounded.InexactFloat64(),
		HasQuotaCalculation:      summary.HasQuotaCalculation,
	}
}
