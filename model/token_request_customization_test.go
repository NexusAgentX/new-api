package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTokenRequestCustomization(t *testing.T) {
	raw := `{
		"version": 1,
		"model_mapping": {
			" gpt-5.5 ": " alias-2 ",
			"alias-2": "glm-5.2"
		}
	}`

	normalized, err := NormalizeTokenRequestCustomization(raw)
	require.NoError(t, err)

	var config TokenRequestCustomization
	require.NoError(t, common.UnmarshalJsonStr(normalized, &config))
	assert.Equal(t, tokenRequestCustomizationVersion, config.Version)
	assert.Equal(t, map[string]string{
		"gpt-5.5": "alias-2",
		"alias-2": "glm-5.2",
	}, config.ModelMapping)

	effectiveModel, mapped, err := ResolveTokenModelMapping(config.ModelMapping, "gpt-5.5")
	require.NoError(t, err)
	assert.True(t, mapped)
	assert.Equal(t, "glm-5.2", effectiveModel)
}

func TestNormalizeTokenRequestCustomizationRejectsInvalidConfigurations(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown field", raw: `{"version":1,"model_mapping":{},"param_override":{}}`},
		{name: "missing version", raw: `{"model_mapping":{"gpt":"glm"}}`},
		{name: "unsupported version", raw: `{"version":2,"model_mapping":{}}`},
		{name: "null mapping", raw: `{"version":1,"model_mapping":null}`},
		{name: "empty target", raw: `{"version":1,"model_mapping":{"gpt":""}}`},
		{name: "duplicate field", raw: `{"version":1,"version":1,"model_mapping":{}}`},
		{name: "duplicate source", raw: `{"version":1,"model_mapping":{"gpt":"glm","gpt":"claude"}}`},
		{name: "duplicate source after trim", raw: `{"version":1,"model_mapping":{"gpt":"glm"," gpt ":"claude"}}`},
		{name: "self cycle", raw: `{"version":1,"model_mapping":{"gpt":"gpt"}}`},
		{name: "indirect cycle", raw: `{"version":1,"model_mapping":{"a":"b","b":"a"}}`},
		{name: "oversized", raw: strings.Repeat("x", maxTokenRequestCustomizationBytes+1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeTokenRequestCustomization(test.raw)
			require.Error(t, err)
		})
	}
}

func TestNormalizeTokenRequestCustomizationClearsEmptyMapping(t *testing.T) {
	normalized, err := NormalizeTokenRequestCustomization(`{"version":1,"model_mapping":{}}`)
	require.NoError(t, err)
	assert.Empty(t, normalized)
}

func TestNormalizeTokenRequestCustomizationAutoGroupPolicy(t *testing.T) {
	raw := `{
		"version": 1,
		"auto_group_policy": {
			"default_rule": {
				"mode": "denylist",
				"groups": [" premium "],
				"min_ratio": 0,
				"max_ratio": 1.5
			},
			"model_rules": {
				" gpt-4o ": {
					"mode": "allowlist",
					"groups": ["preferred", "backup"]
				}
			}
		}
	}`

	normalized, err := NormalizeTokenRequestCustomization(raw)
	require.NoError(t, err)
	customization, err := ParseTokenRequestCustomization(normalized)
	require.NoError(t, err)
	require.NotNil(t, customization.AutoGroupPolicy)

	defaultRule, ok := customization.AutoGroupPolicy.RuleForModel("other-model")
	require.True(t, ok)
	assert.Equal(t, TokenAutoGroupModeDenylist, defaultRule.Mode)
	assert.Equal(t, []string{"premium"}, defaultRule.Groups)
	assert.Equal(t, 0.0, *defaultRule.MinRatio)
	assert.Equal(t, 1.5, *defaultRule.MaxRatio)

	modelRule, ok := customization.AutoGroupPolicy.RuleForModel("gpt-4o")
	require.True(t, ok)
	assert.Equal(t, TokenAutoGroupModeAllowlist, modelRule.Mode)
	assert.Equal(t, []string{"preferred", "backup"}, modelRule.Groups)
}

func TestNormalizeTokenRequestCustomizationRejectsInvalidAutoGroupPolicies(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown policy field", raw: `{"version":1,"auto_group_policy":{"fallback":{}}}`},
		{name: "unknown rule field", raw: `{"version":1,"auto_group_policy":{"default_rule":{"priority":1}}}`},
		{name: "groups without mode", raw: `{"version":1,"auto_group_policy":{"default_rule":{"groups":["a"]}}}`},
		{name: "invalid mode", raw: `{"version":1,"auto_group_policy":{"default_rule":{"mode":"all","groups":["a"]}}}`},
		{name: "negative minimum", raw: `{"version":1,"auto_group_policy":{"default_rule":{"min_ratio":-1}}}`},
		{name: "non finite maximum", raw: `{"version":1,"auto_group_policy":{"default_rule":{"max_ratio":1e999}}}`},
		{name: "inverted interval", raw: `{"version":1,"auto_group_policy":{"default_rule":{"min_ratio":2,"max_ratio":1}}}`},
		{name: "duplicate group", raw: `{"version":1,"auto_group_policy":{"default_rule":{"mode":"allowlist","groups":["a","a"]}}}`},
		{name: "duplicate model after trim", raw: `{"version":1,"auto_group_policy":{"model_rules":{"gpt":{}," gpt ":{}}}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeTokenRequestCustomization(test.raw)
			require.Error(t, err)
		})
	}
}
