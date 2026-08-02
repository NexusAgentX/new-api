package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateFailureSampleSizeOption(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "minimum", value: "1", valid: true},
		{name: "maximum", value: "16", valid: true},
		{name: "zero", value: "0"},
		{name: "above maximum", value: "17"},
		{name: "not an integer", value: "1.5"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateOptionValue("first_response_timeout_setting.failure_sample_max_mb", test.value)
			if test.valid {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
		})
	}
}
