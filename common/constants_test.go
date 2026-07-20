package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFrontendPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "top up path",
			path:     "/console/topup",
			expected: "/wallet",
		},
		{
			name:     "usage log path with query",
			path:     "/console/log?type=2",
			expected: "/usage-logs?type=2",
		},
		{
			name:     "personal path with suffix",
			path:     "/console/personal/security",
			expected: "/profile/security",
		},
		{
			name:     "unrelated path",
			path:     "/console/channel",
			expected: "/console/channel",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, FrontendPath(test.path))
		})
	}
}
