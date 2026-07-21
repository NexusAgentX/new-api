package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTokenPersistsRequestCustomizationAndAutoGroupPolicySeparately(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedToken(t, db, 1, "customized-token", "custom1234token5678")

	body := map[string]any{
		"id":                   token.Id,
		"name":                 token.Name,
		"expired_time":         -1,
		"remain_quota":         100,
		"unlimited_quota":      true,
		"model_limits_enabled": false,
		"model_limits":         "",
		"group":                "default",
		"cross_group_retry":    false,
		"request_customization": `{
			"version": 1,
			"model_mapping": {" gpt-5.5 ": " glm-5.2 "}
		}`,
		"auto_group_policy": `{
			"default_rule": {
				"mode": "allowlist",
				"groups": [" preferred "]
			}
		}`,
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/token/", body, 1)
	UpdateToken(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	updated, err := model.GetTokenByIds(token.Id, 1)
	require.NoError(t, err)
	customization, err := model.ParseTokenRequestCustomization(updated.RequestCustomization)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"gpt-5.5": "glm-5.2"}, customization.ModelMapping)
	assert.NotContains(t, updated.RequestCustomization, "auto_group_policy")

	policy, err := model.ParseTokenAutoGroupPolicy(updated.AutoGroupPolicy)
	require.NoError(t, err)
	require.NotNil(t, policy)
	rule, ok := policy.RuleForModel("gpt-5.5")
	require.True(t, ok)
	assert.Equal(t, model.TokenAutoGroupModeAllowlist, rule.Mode)
	assert.Equal(t, []string{"preferred"}, rule.Groups)
}

func TestUpdateTokenRejectsCyclicRequestCustomization(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedToken(t, db, 1, "customized-token", "cycle1234token5678")

	body := map[string]any{
		"id":                    token.Id,
		"name":                  token.Name,
		"expired_time":          -1,
		"remain_quota":          100,
		"unlimited_quota":       true,
		"model_limits_enabled":  false,
		"model_limits":          "",
		"group":                 "default",
		"cross_group_retry":     false,
		"request_customization": `{"version":1,"model_mapping":{"a":"b","b":"a"}}`,
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/token/", body, 1)
	UpdateToken(ctx)

	response := decodeAPIResponse(t, recorder)
	assert.False(t, response.Success)
	assert.NotEmpty(t, response.Message)

	unchanged, err := model.GetTokenByIds(token.Id, 1)
	require.NoError(t, err)
	assert.Empty(t, unchanged.RequestCustomization)
	assert.NotContains(t, recorder.Body.String(), token.Key)
}
