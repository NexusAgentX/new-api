package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelValidateSettingsAdmissionLimits(t *testing.T) {
	tests := []struct {
		name    string
		setting dto.ChannelSettings
		wantErr string
	}{
		{
			name:    "channel admission limits are valid",
			setting: dto.ChannelSettings{MaxConcurrency: 20, RPMLimit: 120},
		},
		{
			name:    "negative channel admission limit rejected",
			setting: dto.ChannelSettings{RPMLimit: -1},
			wantErr: "rpm_limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{}
			channel.SetSetting(tt.setting)
			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAdvancedCustomChannelRequiresModelListRouteOnlyWhenUpdateChecksEnabled(t *testing.T) {
	inferenceRoute := dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
		Converter:    "none",
	}

	tests := []struct {
		name          string
		checksEnabled bool
		routes        []dto.AdvancedCustomRoute
		wantErr       string
	}{
		{
			name:   "legacy channel without discovery route remains valid",
			routes: []dto.AdvancedCustomRoute{inferenceRoute},
		},
		{
			name:          "enabled checks require discovery route",
			checksEnabled: true,
			routes:        []dto.AdvancedCustomRoute{inferenceRoute},
			wantErr:       dto.AdvancedCustomModelListPath,
		},
		{
			name:          "enabled checks accept discovery route",
			checksEnabled: true,
			routes: []dto.AdvancedCustomRoute{
				inferenceRoute,
				{
					IncomingPath: dto.AdvancedCustomModelListPath,
					UpstreamPath: dto.AdvancedCustomModelListPath,
					Converter:    "none",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{Type: constant.ChannelTypeAdvancedCustom}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				UpstreamModelUpdateCheckEnabled: tt.checksEnabled,
				AdvancedCustom: &dto.AdvancedCustomConfig{
					Routes: tt.routes,
				},
			})

			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestChannelValidateSettingsChannelTestProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setting string
		valid   bool
	}{
		{name: "legacy setting", setting: `{}`, valid: true},
		{name: "responses stream profile", setting: `{"test_endpoint_type":"openai-response","test_stream":true,"test_sample_tokens":2048,"test_prepend_nonce":true,"test_disable_threshold_seconds":120}`, valid: true},
		{name: "unknown endpoint", setting: `{"test_endpoint_type":"unknown"}`},
		{name: "streaming embeddings", setting: `{"test_endpoint_type":"embeddings","test_stream":true}`},
		{name: "maximum sample size", setting: `{"test_sample_tokens":8192}`, valid: true},
		{name: "sample size above maximum", setting: `{"test_sample_tokens":8193}`},
		{name: "negative sample size", setting: `{"test_sample_tokens":-1}`},
		{name: "disabled response time threshold", setting: `{"test_disable_threshold_seconds":0}`, valid: true},
		{name: "negative response time threshold", setting: `{"test_disable_threshold_seconds":-1}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			channel := Channel{Setting: &test.setting}
			err := channel.ValidateSettings()
			if test.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}

func TestChannelValidateSettingsFirstResponseTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setting string
		valid   bool
	}{
		{name: "unset", setting: `{}`, valid: true},
		{name: "inherit with zero", setting: `{"first_response_timeout_seconds":0}`, valid: true},
		{name: "minimum", setting: `{"first_response_timeout_seconds":1}`, valid: true},
		{name: "maximum", setting: `{"first_response_timeout_seconds":300}`, valid: true},
		{name: "negative", setting: `{"first_response_timeout_seconds":-1}`, valid: false},
		{name: "above maximum", setting: `{"first_response_timeout_seconds":301}`, valid: false},
		{name: "non integer", setting: `{"first_response_timeout_seconds":1.5}`, valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			channel := Channel{Setting: &test.setting}
			err := channel.ValidateSettings()
			if test.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
