package channel

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	constant2 "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}

var initDoRequestTestContext sync.Once

func newDoRequestTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	initDoRequestTestContext.Do(func() {
		service.InitHttpClient()
		gin.SetMode(gin.TestMode)
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return ctx, recorder
}

func TestFirstResponseTimeoutCancelsStreamBeforeDownstreamCommit(t *testing.T) {
	originalStreamingTimeout := constant2.StreamingTimeout
	constant2.StreamingTimeout = 30
	t.Cleanup(func() { constant2.StreamingTimeout = originalStreamingTimeout })
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	original := *setting
	t.Cleanup(func() { *setting = original })
	setting.RetryEnabled = true
	setting.DisableEnabled = false
	setting.TimeoutSeconds = 1

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()

	ctx, recorder := newDoRequestTestContext()
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAIResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	req, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{"model":"gpt-test"}`))
	require.NoError(t, err)

	resp, err := DoRequest(ctx, req, info)
	require.NoError(t, err)
	t.Cleanup(func() { info.EndFirstResponseAttempt() })

	timeoutErr := helper.StreamScannerHandler(ctx, resp, info, func(string, *helper.StreamResult) {})
	require.NotNil(t, timeoutErr)
	require.Equal(t, 524, timeoutErr.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamFirstResponseTimeout, timeoutErr.GetErrorCode())
	require.False(t, recorder.Flushed)
	require.Empty(t, recorder.Body.String())
}

func TestFirstResponseEventStopsTimeout(t *testing.T) {
	originalStreamingTimeout := constant2.StreamingTimeout
	constant2.StreamingTimeout = 30
	t.Cleanup(func() { constant2.StreamingTimeout = originalStreamingTimeout })
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	original := *setting
	t.Cleanup(func() { *setting = original })
	setting.RetryEnabled = true
	setting.DisableEnabled = false
	setting.TimeoutSeconds = 1

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\"}\n\n"))
		w.(http.Flusher).Flush()
		time.Sleep(1200 * time.Millisecond)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	ctx, _ := newDoRequestTestContext()
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAIResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	req, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{"model":"gpt-test"}`))
	require.NoError(t, err)

	resp, err := DoRequest(ctx, req, info)
	require.NoError(t, err)
	t.Cleanup(func() { info.EndFirstResponseAttempt() })
	var received int

	timeoutErr := helper.StreamScannerHandler(ctx, resp, info, func(string, *helper.StreamResult) {
		received++
	})
	require.Nil(t, timeoutErr)
	require.Equal(t, 1, received)
	require.False(t, info.IsFirstResponseAttemptTimedOut())
}

func TestDoRequestEmitsServerTimingBreakdown(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer upstream.Close()

	ctx, recorder := newDoRequestTestContext()
	ctx.Set(string(constant2.ContextKeyRequestStartTime), time.Now().Add(-50*time.Millisecond))

	req, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	require.NoError(t, err)

	resp, err := DoRequest(ctx, req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	defer resp.Body.Close()

	header := recorder.Header().Get("Server-Timing")
	require.Regexp(t, `(?:^|, )upstream;dur=\d+`, header)
	require.Regexp(t, `(?:^|, )upstream-prefill;dur=\d+`, header)

	gateway := regexp.MustCompile(`(?:^|, )gateway;dur=(\d+)`).FindStringSubmatch(header)
	require.Len(t, gateway, 2, "gateway dur should be reported when the request start time is known")
	gatewayMs, err := strconv.Atoi(gateway[1])
	require.NoError(t, err)
	// The start time was pinned 50ms in the past; scheduling jitter can only
	// increase the measured gateway overhead, never shrink it below that.
	require.GreaterOrEqual(t, gatewayMs, 50)
}

func TestDoRequestOmitsGatewayMetricWithoutStartTime(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ctx, recorder := newDoRequestTestContext()

	req, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	require.NoError(t, err)

	resp, err := DoRequest(ctx, req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	defer resp.Body.Close()

	header := recorder.Header().Get("Server-Timing")
	require.Regexp(t, `(?:^|, )upstream;dur=\d+`, header)
	require.NotContains(t, header, "gateway;dur=")
}

func TestDoRequestSplitsReceiveWindowFromGatewayProcessing(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ctx, recorder := newDoRequestTestContext()
	start := time.Now()
	ctx.Set(string(constant2.ContextKeyRequestArrivedAt), start.Add(-120*time.Millisecond))
	ctx.Set(string(constant2.ContextKeyRequestBodyReceivedAt), start.Add(-50*time.Millisecond))

	req, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	require.NoError(t, err)

	resp, err := DoRequest(ctx, req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	defer resp.Body.Close()

	header := recorder.Header().Get("Server-Timing")
	recv := regexp.MustCompile(`(?:^|, )recv;dur=(\d+)`).FindStringSubmatch(header)
	require.Len(t, recv, 2, "recv dur should be reported when the body receive time is known")
	recvMs, err := strconv.Atoi(recv[1])
	require.NoError(t, err)
	require.GreaterOrEqual(t, recvMs, 70)

	gateway := regexp.MustCompile(`(?:^|, )gateway;dur=(\d+)`).FindStringSubmatch(header)
	require.Len(t, gateway, 2)
	gatewayMs, err := strconv.Atoi(gateway[1])
	require.NoError(t, err)
	require.GreaterOrEqual(t, gatewayMs, 50, "gateway measures from body-received to forward, not from distributor start")
}
