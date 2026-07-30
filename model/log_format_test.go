package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

func TestFormatUserLogsPreservesRequestDegradationForOwners(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"request_degradation": map[string]interface{}{
			"applied":                 true,
			"reason":                  "non_replayable_reasoning",
			"dropped_reasoning_items": 4,
		},
		"admin_info": map[string]interface{}{
			"use_channel": []int{1, 2},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo)
	degradation, ok := parsed["request_degradation"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, degradation["applied"])
	require.Equal(t, "non_replayable_reasoning", degradation["reason"])
	require.Equal(t, float64(4), degradation["dropped_reasoning_items"])
}

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}
