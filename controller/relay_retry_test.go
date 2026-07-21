package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryFirstResponseTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	original := *setting
	t.Cleanup(func() { *setting = original })

	testCases := []struct {
		name            string
		enabled         bool
		retriesLeft     int
		specificChannel bool
		want            bool
	}{
		{name: "enabled bypasses generic 524 skip", enabled: true, retriesLeft: 1, want: true},
		{name: "disabled", enabled: false, retriesLeft: 1, want: false},
		{name: "retry budget exhausted", enabled: true, retriesLeft: 0, want: false},
		{name: "specific channel remains fixed", enabled: true, retriesLeft: 1, specificChannel: true, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setting.RetryEnabled = testCase.enabled
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			if testCase.specificChannel {
				ctx.Set("specific_channel_id", 1)
			}

			err := types.NewUpstreamFirstResponseTimeoutError(20)
			require.Equal(t, 524, err.StatusCode)
			assert.Equal(t, testCase.want, shouldRetry(ctx, err, testCase.retriesLeft))
		})
	}
}
