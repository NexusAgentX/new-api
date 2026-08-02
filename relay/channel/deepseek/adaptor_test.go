package deepseek

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rootconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDeepSeekResponsesTestContext(t *testing.T, body string, stream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := rootconstant.StreamingTimeout
	rootconstant.StreamingTimeout = 30
	t.Cleanup(func() { rootconstant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{Model: "deepseek-v4-pro", Stream: &stream}
	info := relaycommon.GenRelayInfoResponses(c, request)
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: request.Model}
	info.DisablePing = true

	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{contentType}},
	}
	return c, recorder, resp, info
}

func TestAdaptorConvertsResponsesRequestAndURL(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		RelayFormat:     types.RelayFormatOpenAIResponses,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.deepseek.com", UpstreamModelName: "deepseek-v4-pro-none"},
		ReasoningEffort: "",
	}

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://api.deepseek.com/responses", requestURL)

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model: "deepseek-v4-pro-none",
	})
	require.NoError(t, err)
	convertedRequest, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.Equal(t, "deepseek-v4-pro", convertedRequest.Model)
	require.NotNil(t, convertedRequest.Reasoning)
	assert.Equal(t, "none", convertedRequest.Reasoning.Effort)
	assert.Equal(t, "deepseek-v4-pro", info.UpstreamModelName)
	assert.Equal(t, "none", info.ReasoningEffort)
}

func TestAdaptorHandlesResponsesJSON(t *testing.T) {
	body := `{"id":"resp_deepseek","object":"response","status":"completed","model":"deepseek-v4-pro","output":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"OK","annotations":[]}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`
	c, recorder, resp, info := newDeepSeekResponsesTestContext(t, body, false)

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.Nil(t, apiErr)
	responsesUsage, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 2, responsesUsage.PromptTokens)
	assert.Equal(t, 1, responsesUsage.CompletionTokens)
	assert.Equal(t, 3, responsesUsage.TotalTokens)
	assert.JSONEq(t, body, recorder.Body.String())
}

func TestAdaptorHandlesResponsesStream(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_deepseek","status":"in_progress"}}`,
		`data: {"type":"response.output_text.delta","delta":"OK"}`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newDeepSeekResponsesTestContext(t, body, true)

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.Nil(t, apiErr)
	responsesUsage, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 2, responsesUsage.PromptTokens)
	assert.Equal(t, 1, responsesUsage.CompletionTokens)
	assert.Equal(t, 3, responsesUsage.TotalTokens)
	assert.Contains(t, recorder.Body.String(), "event: response.created")
	assert.Contains(t, recorder.Body.String(), "event: response.output_text.delta")
	assert.Contains(t, recorder.Body.String(), "event: response.completed")
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.False(t, info.StreamStatus.HasErrors())
}
