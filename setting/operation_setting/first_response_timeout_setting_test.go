package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateFirstResponseDisableMinTimeoutAttempts(t *testing.T) {
	tests := []struct {
		name     string
		attempts int
		valid    bool
	}{
		{name: "minimum", attempts: MinFirstResponseDisableMinTimeoutAttempts, valid: true},
		{name: "maximum", attempts: MaxFirstResponseDisableMinTimeoutAttempts, valid: true},
		{name: "below minimum", attempts: MinFirstResponseDisableMinTimeoutAttempts - 1, valid: false},
		{name: "above maximum", attempts: MaxFirstResponseDisableMinTimeoutAttempts + 1, valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.valid {
				assert.NoError(t, ValidateFirstResponseDisableMinTimeoutAttempts(test.attempts))
				return
			}
			assert.Error(t, ValidateFirstResponseDisableMinTimeoutAttempts(test.attempts))
		})
	}
}
