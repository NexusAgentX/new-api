package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newNativeResponsesStreamTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stream := true
	info := relaycommon.GenRelayInfoResponses(c, &dto.OpenAIResponsesRequest{Stream: &stream})
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}
	info.DisablePing = true
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	return c, recorder, resp, info
}

func TestOaiResponsesHandlerNormalizesItemIDsWithoutDroppingExtensions(t *testing.T) {
	body := []byte(`{
		"id":"resp_1",
		"status":"completed",
		"vendor_response":{"keep":true},
		"output":[{"type":"function_call","id":"item_function","call_id":"call_1","name":"lookup","arguments":"{}","vendor_item":7}],
		"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}
	}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-test",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}

	usage, apiErr := OaiResponsesHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	var output map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &output))
	assert.Equal(t, map[string]any{"keep": true}, output["vendor_response"])
	item := output["output"].([]any)[0].(map[string]any)
	assert.Equal(t, "fc_function", item["id"])
	assert.Equal(t, "call_1", item["call_id"])
	assert.Equal(t, float64(7), item["vendor_item"])
	require.NotNil(t, info.ResponsesCompatibility)
	assert.Equal(t, 1, info.ResponsesCompatibility.NormalizedResponseItemIDs)
}

func TestOaiResponsesStreamHandlerNormalizesFullItemEventChain(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item_function","call_id":"call_1","vendor_item":true}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"item_function","delta":"{}","vendor_event":true}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"item_function","call_id":"call_1","arguments":"{}"}}`,
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","id":"item_function","call_id":"call_1","arguments":"{}","vendor_final":true}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newNativeResponsesStreamTestContext(t, body)

	usage, streamError := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, streamError)
	require.NotNil(t, usage)
	assert.NotContains(t, recorder.Body.String(), `"item_function"`)
	assert.Contains(t, recorder.Body.String(), `"fc_function"`)
	assert.Contains(t, recorder.Body.String(), `"vendor_event":true`)
	assert.Contains(t, recorder.Body.String(), `"vendor_final":true`)
	require.NotNil(t, info.ResponsesCompatibility)
	assert.Equal(t, 1, info.ResponsesCompatibility.NormalizedResponseItemIDs)
}

func TestOaiResponsesStreamHandlerHonorsExplicitCompatibilityDisable(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item_function","call_id":"call_1"}}`,
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","id":"item_function","call_id":"call_1"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newNativeResponsesStreamTestContext(t, body)
	disabled := false
	info.ChannelSetting.ResponsesCompatibilityFix = &disabled

	_, streamError := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, streamError)
	assert.Contains(t, recorder.Body.String(), `"item_function"`)
	assert.NotContains(t, recorder.Body.String(), `"fc_function"`)
	assert.Nil(t, info.ResponsesCompatibility)
}

func TestOaiResponsesStreamHandlerReturnsFirstRateLimitEventForRetry(t *testing.T) {
	testCases := []struct {
		name  string
		event string
	}{
		{
			name:  "response failed envelope",
			event: `{"type":"response.failed","response":{"status":"failed","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"Concurrency limit exceeded for account, please retry later"}}}`,
		},
		{
			name:  "response error object",
			event: `{"type":"response.error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"Too many pending requests, please retry later"}}`,
		},
		{
			name:  "top level error event",
			event: `{"type":"error","code":"rate_limit_exceeded","message":"Concurrency limit exceeded for account, please retry later"}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			body := "data: " + testCase.event + "\n\ndata: [DONE]\n\n"
			c, recorder, resp, info := newNativeResponsesStreamTestContext(t, body)

			usage, streamError := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, streamError)
			assert.Equal(t, http.StatusTooManyRequests, streamError.StatusCode)
			assert.Equal(t, types.ErrorTypeOpenAIError, streamError.GetErrorType())
			assert.Equal(t, types.ErrorCode("rate_limit_exceeded"), streamError.GetErrorCode())
			assert.Contains(t, streamError.Error(), "retry later")
			assert.Empty(t, recorder.Body.String())
			require.NotNil(t, info.StreamStatus)
			assert.True(t, info.StreamStatus.HasErrors())
			require.NotEmpty(t, info.StreamStatus.Errors)
			assert.Contains(t, info.StreamStatus.Errors[0].Message, "retry later")
			assert.False(t, info.HasSendResponse())
		})
	}
}

func TestOaiResponsesStreamHandlerPreservesFailureAfterStreamStarts(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`data: {"type":"response.failed","response":{"status":"failed","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"Concurrency limit exceeded for account, please retry later"}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newNativeResponsesStreamTestContext(t, body)

	usage, streamError := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, streamError)
	require.NotNil(t, usage)
	assert.Contains(t, recorder.Body.String(), `event: response.created`)
	assert.Contains(t, recorder.Body.String(), `event: response.failed`)
	require.NotNil(t, info.StreamStatus)
	assert.True(t, info.StreamStatus.HasErrors())
	require.NotEmpty(t, info.StreamStatus.Errors)
	assert.Contains(t, info.StreamStatus.Errors[0].Message, "retry later")
}

func TestOaiResponsesStreamHandlerForwardsCompletedUsage(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`data: {"type":"response.output_text.delta","delta":"OK"}`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newNativeResponsesStreamTestContext(t, body)

	usage, streamError := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, streamError)
	require.NotNil(t, usage)
	assert.Equal(t, 2, usage.PromptTokens)
	assert.Equal(t, 1, usage.CompletionTokens)
	assert.Equal(t, 3, usage.TotalTokens)
	assert.Contains(t, recorder.Body.String(), `event: response.completed`)
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.False(t, info.StreamStatus.HasErrors())
}
