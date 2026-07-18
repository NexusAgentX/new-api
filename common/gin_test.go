package common

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetRequestBodyRecordsReceiveCompletion(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.6-sol"}`))

	before := time.Now()
	_, err := GetRequestBody(ctx)
	require.NoError(t, err)
	receivedAt := ctx.GetTime(string(constant.ContextKeyRequestBodyReceivedAt))

	require.False(t, receivedAt.IsZero(), "GetRequestBody should record when the client body was fully buffered")
	require.GreaterOrEqual(t, receivedAt.Compare(before), 0)
	require.LessOrEqual(t, receivedAt.Compare(time.Now()), 0)
}

func TestGetRequestBodyKeepsOriginalReceiveTimeFromCache(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.6-sol"}`))

	_, err := GetRequestBody(ctx)
	require.NoError(t, err)
	first := ctx.GetTime(string(constant.ContextKeyRequestBodyReceivedAt))
	require.False(t, first.IsZero())

	_, err = GetRequestBody(ctx)
	require.NoError(t, err)
	require.Equal(t, first, ctx.GetTime(string(constant.ContextKeyRequestBodyReceivedAt)), "cached reads must not move the receive timestamp")
}
