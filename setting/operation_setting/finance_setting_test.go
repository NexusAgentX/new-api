package operation_setting

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateInfrastructureDailyCostUSD(t *testing.T) {
	for _, value := range []float64{0, 0.01, MaxInfrastructureDailyCostUSD} {
		assert.NoError(t, ValidateInfrastructureDailyCostUSD(value))
	}
	for _, value := range []float64{-0.01, MaxInfrastructureDailyCostUSD + 1, math.NaN(), math.Inf(1)} {
		assert.Error(t, ValidateInfrastructureDailyCostUSD(value))
	}
}
