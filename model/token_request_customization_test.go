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
