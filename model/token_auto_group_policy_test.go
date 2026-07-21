package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTokenAutoGroupPolicy(t *testing.T) {
	raw := `{
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
	}`

	normalized, err := NormalizeTokenAutoGroupPolicy(raw)
	require.NoError(t, err)
	policy, err := ParseTokenAutoGroupPolicy(normalized)
	require.NoError(t, err)
	require.NotNil(t, policy)

	defaultRule, ok := policy.RuleForModel("other-model")
	require.True(t, ok)
	assert.Equal(t, TokenAutoGroupModeDenylist, defaultRule.Mode)
	assert.Equal(t, []string{"premium"}, defaultRule.Groups)
	assert.Equal(t, 0.0, *defaultRule.MinRatio)
	assert.Equal(t, 1.5, *defaultRule.MaxRatio)

	modelRule, ok := policy.RuleForModel("gpt-4o")
	require.True(t, ok)
	assert.Equal(t, TokenAutoGroupModeAllowlist, modelRule.Mode)
	assert.Equal(t, []string{"preferred", "backup"}, modelRule.Groups)
}

func TestNormalizeTokenAutoGroupPolicyRejectsInvalidConfigurations(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown policy field", raw: `{"fallback":{}}`},
		{name: "unknown rule field", raw: `{"default_rule":{"priority":1}}`},
		{name: "groups without mode", raw: `{"default_rule":{"groups":["a"]}}`},
		{name: "invalid mode", raw: `{"default_rule":{"mode":"all","groups":["a"]}}`},
		{name: "negative minimum", raw: `{"default_rule":{"min_ratio":-1}}`},
		{name: "non finite maximum", raw: `{"default_rule":{"max_ratio":1e999}}`},
		{name: "inverted interval", raw: `{"default_rule":{"min_ratio":2,"max_ratio":1}}`},
		{name: "duplicate group", raw: `{"default_rule":{"mode":"allowlist","groups":["a","a"]}}`},
		{name: "duplicate model after trim", raw: `{"model_rules":{"gpt":{}," gpt ":{}}}`},
		{name: "oversized", raw: strings.Repeat("x", maxTokenAutoGroupPolicyBytes+1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeTokenAutoGroupPolicy(test.raw)
			require.Error(t, err)
		})
	}
}

func TestNormalizeTokenAutoGroupPolicyClearsEmptyPolicy(t *testing.T) {
	normalized, err := NormalizeTokenAutoGroupPolicy(`{}`)
	require.NoError(t, err)
	assert.Empty(t, normalized)
}
