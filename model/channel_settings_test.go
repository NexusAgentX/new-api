package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
