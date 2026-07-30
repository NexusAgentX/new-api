package helper

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResponsesSanitizerContext(t *testing.T, body string) (*gin.Context, *dto.OpenAIResponsesRequest) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() {
		common.CleanupBodyStorage(c)
	})
	request, err := GetAndValidateResponsesRequest(c)
	require.NoError(t, err)
	return c, request
}

func TestSanitizeResponsesRequestRemovesOnlyNonReplayableReasoning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{
		"model":"gpt-test",
		"store":false,
		"vendor_extension":{"preserve":true},
		"input":[
			{"type":"message","id":"msg_1","role":"user","content":"hello"},
			{"type":"reasoning","id":"rs_missing","summary":[{"type":"summary_text","text":"missing"}]},
			{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"},
			{"type":"reasoning","id":"rs_null","encrypted_content":null},
			{"type":"function_call_output","id":"out_1","call_id":"call_1","output":"ok"},
			{"type":"reasoning","id":"rs_empty","encrypted_content":""},
			{"type":"reasoning","id":"rs_valid","encrypted_content":"ciphertext","summary":[]},
			{"type":"reasoning","id":"rs_numeric","encrypted_content":0},
			{"type":"message","id":"msg_2","role":"assistant","content":"done"}
		]
	}`
	c, request := newResponsesSanitizerContext(t, body)

	degradation, err := SanitizeResponsesRequest(c, request)
	require.NoError(t, err)
	require.NotNil(t, degradation)
	assert.True(t, degradation.Applied)
	assert.Equal(t, types.RequestDegradationReasonNonReplayableReasoning, degradation.Reason)
	assert.Equal(t, 3, degradation.DroppedReasoningItems)

	var inputItems []struct {
		ID               string `json:"id"`
		CallID           string `json:"call_id"`
		EncryptedContent any    `json:"encrypted_content"`
	}
	require.NoError(t, common.Unmarshal(request.Input, &inputItems))
	require.Len(t, inputItems, 6)
	assert.Equal(t, []string{"msg_1", "fc_1", "out_1", "rs_valid", "rs_numeric", "msg_2"}, []string{
		inputItems[0].ID,
		inputItems[1].ID,
		inputItems[2].ID,
		inputItems[3].ID,
		inputItems[4].ID,
		inputItems[5].ID,
	})
	assert.Equal(t, "call_1", inputItems[1].CallID)
	assert.Equal(t, "call_1", inputItems[2].CallID)
	assert.Equal(t, "ciphertext", inputItems[3].EncryptedContent)
	assert.Equal(t, float64(0), inputItems[4].EncryptedContent)

	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	sanitizedBody, err := storage.Bytes()
	require.NoError(t, err)
	var topLevel map[string]any
	require.NoError(t, common.Unmarshal(sanitizedBody, &topLevel))
	assert.Equal(t, map[string]any{"preserve": true}, topLevel["vendor_extension"])
	assert.Equal(t, int64(len(sanitizedBody)), c.Request.ContentLength)
	assert.Equal(t, strconv.Itoa(len(sanitizedBody)), c.Request.Header.Get("Content-Length"))

	for attempt := 0; attempt < 2; attempt++ {
		attemptStorage, storageErr := common.GetBodyStorage(c)
		require.NoError(t, storageErr)
		c.Request.Body = io.NopCloser(attemptStorage)
		forwardedBody, readErr := io.ReadAll(c.Request.Body)
		require.NoError(t, readErr)
		assert.JSONEq(t, string(sanitizedBody), string(forwardedBody), "attempt %d must reuse the sanitized body", attempt)
	}
	assert.Equal(t, 3, degradation.DroppedReasoningItems)
}

func TestSanitizeResponsesRequestKeepsOutOfScopeInputsByteIdentical(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "store omitted",
			body: `{"model":"gpt-test","input":[{"type":"reasoning","id":"rs_1"}]}`,
		},
		{
			name: "store true",
			body: `{"model":"gpt-test","store":true,"input":[{"type":"reasoning","id":"rs_1"}]}`,
		},
		{
			name: "store null",
			body: `{"model":"gpt-test","store":null,"input":[{"type":"reasoning","id":"rs_1"}]}`,
		},
		{
			name: "store string false",
			body: `{"model":"gpt-test","store":"false","input":[{"type":"reasoning","id":"rs_1"}]}`,
		},
		{
			name: "scalar input",
			body: `{"model":"gpt-test","store":false,"input":"hello"}`,
		},
		{
			name: "object input",
			body: `{"model":"gpt-test","store":false,"input":{"role":"user","content":"hello"}}`,
		},
		{
			name: "nonempty and invalid encrypted content stays upstream validated",
			body: `{"model":"gpt-test","store":false,"input":[{"type":"reasoning","id":"rs_text","encrypted_content":" "},{"type":"reasoning","id":"rs_number","encrypted_content":0},{"type":42,"id":"unknown"}]}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, request := newResponsesSanitizerContext(t, testCase.body)
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			before, err := storage.Bytes()
			require.NoError(t, err)
			before = bytes.Clone(before)

			degradation, err := SanitizeResponsesRequest(c, request)
			require.NoError(t, err)
			assert.Nil(t, degradation)

			storage, err = common.GetBodyStorage(c)
			require.NoError(t, err)
			after, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}
}

func TestMalformedResponsesJSONKeepsValidationErrorPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-test","store":false,"input":[`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() {
		common.CleanupBodyStorage(c)
	})

	_, err := GetAndValidateResponsesRequest(c)
	require.Error(t, err)
}
