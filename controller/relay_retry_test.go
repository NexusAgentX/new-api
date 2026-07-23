package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestContextError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	requestCtx, cancel := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)

	require.Nil(t, requestContextError(ctx))
	cancel()

	err := requestContextError(ctx)
	require.NotNil(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 499, err.StatusCode)
	assert.Equal(t, types.ErrorCodeRequestCanceled, err.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(err))
	assert.False(t, types.IsRecordErrorLog(err))
}

func TestShouldRetryStopsWhenRequestContextDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testCases := []struct {
		name        string
		requestDone bool
		err         *types.NewAPIError
		want        bool
	}{
		{
			name: "active request retries generic 500",
			err:  types.NewErrorWithStatusCode(context.Canceled, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError),
			want: true,
		},
		{
			name:        "canceled request stops generic 500 retry",
			requestDone: true,
			err:         types.NewErrorWithStatusCode(context.Canceled, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError),
		},
		{
			name:        "canceled request stops channel error retry",
			requestDone: true,
			err:         types.NewErrorWithStatusCode(context.Canceled, types.ErrorCodeChannelInvalidKey, http.StatusInternalServerError),
		},
		{
			name:        "canceled request stops first response timeout retry",
			requestDone: true,
			err:         types.NewUpstreamFirstResponseTimeoutError(20),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			requestCtx, cancel := context.WithCancel(context.Background())
			if testCase.requestDone {
				cancel()
			} else {
				t.Cleanup(cancel)
			}
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)

			assert.Equal(t, testCase.want, shouldRetry(ctx, testCase.err, 1))
		})
	}
}

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
