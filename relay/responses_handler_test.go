package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runResponsesRequestOrderTest(
	t *testing.T,
	requestBody string,
	channelSetting dto.ChannelSettings,
	paramOverride map[string]interface{},
) []byte {
	t.Helper()
	requestCh := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestCh <- body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"type":"upstream_error","message":"stop after capture"}}`))
	}))
	t.Cleanup(upstream.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	common.SetContextKey(c, constant.ContextKeyChannelName, "responses-order-test")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "sk-test")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channelSetting)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, paramOverride)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, map[string]interface{}{})
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-test")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	request, err := helper.GetAndValidateResponsesRequest(c)
	require.NoError(t, err)
	attempt, err := helper.PrepareResponsesRequestAttempt(
		request,
		channelSetting,
		channelSetting.PassThroughBodyEnabled,
	)
	require.NoError(t, err)
	info := relaycommon.GenRelayInfoResponses(c, attempt.Request)
	info.RecordResponsesRequestCompatibility(attempt.Compatibility, attempt.Degradation)

	apiErr := ResponsesHelper(c, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	return <-requestCh
}

func TestResponsesRequestCompatibilityRunsBeforeParamOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := runResponsesRequestOrderTest(
		t,
		`{"model":"gpt-test","store":false,"input":[{"type":"function_call","id":"item_function","call_id":"call_1"}]}`,
		dto.ChannelSettings{},
		map[string]interface{}{
			"operations": []interface{}{
				map[string]interface{}{
					"path":  "input.0.id",
					"mode":  "set",
					"value": "fc_override",
				},
			},
		},
	)

	assert.Contains(t, string(body), `"id":"fc_override"`)
	assert.NotContains(t, string(body), `"item_function"`)
}

func TestResponsesRequestBodyPassthroughSkipsCompatibilityAndParamOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := `{"model":"gpt-test","store":false,"input":[{"type":"function_call","id":"item_function","call_id":"call_1"}],"vendor_extension":{"keep":true}}`
	body := runResponsesRequestOrderTest(
		t,
		original,
		dto.ChannelSettings{PassThroughBodyEnabled: true},
		map[string]interface{}{
			"operations": []interface{}{
				map[string]interface{}{
					"path":  "input.0.id",
					"mode":  "set",
					"value": "fc_override",
				},
			},
		},
	)

	assert.JSONEq(t, original, string(body))
	assert.Contains(t, string(body), `"item_function"`)
	assert.NotContains(t, string(body), `"fc_override"`)
}
