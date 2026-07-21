package operation_setting

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/setting/config"
)

const MaxInfrastructureDailyCostUSD = 1_000_000_000

type FinanceSetting struct {
	InfrastructureDailyCostUSD float64 `json:"infrastructure_daily_cost_usd"`
}

var financeSetting = FinanceSetting{}

func init() {
	config.GlobalConfig.Register("finance_setting", &financeSetting)
}

func GetFinanceSetting() *FinanceSetting {
	return &financeSetting
}

func ValidateInfrastructureDailyCostUSD(value float64) error {
	if value < 0 || value > MaxInfrastructureDailyCostUSD || math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("infrastructure daily cost must be between 0 and %d USD", MaxInfrastructureDailyCostUSD)
	}
	return nil
}
