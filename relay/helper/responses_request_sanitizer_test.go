package helper

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func responsesRequestFromJSON(t *testing.T, body string) *dto.OpenAIResponsesRequest {
	t.Helper()
	var request dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(body), &request))
	return &request
}

func responsesAttemptItems(t *testing.T, request *dto.OpenAIResponsesRequest) []map[string]any {
	t.Helper()
	var items []map[string]any
	require.NoError(t, common.Unmarshal(request.Input, &items))
	return items
}

func TestPrepareResponsesRequestAttemptAppliesDefaultReplayProtectionAndItemIDs(t *testing.T) {
	original := responsesRequestFromJSON(t, `{
		"model":"gpt-test",
		"store":false,
		"input":[
			{"type":"message","id":"item_message","role":"user","content":"hello","vendor_field":true},
			{"type":"reasoning","id":"item_missing","summary":[{"type":"summary_text","text":"missing"}]},
			{"type":"function_call","id":"item_function","call_id":"call_1","name":"lookup","arguments":"{}"},
			{"type":"reasoning","id":"item_null","encrypted_content":null},
			{"type":"function_call_output","id":"item_output","call_id":"call_1","output":"ok"},
			{"type":"reasoning","id":"item_empty","encrypted_content":""},
			{"type":"reasoning","id":"item_valid","encrypted_content":"ciphertext","summary":[]},
			{"type":"reasoning","id":"item_numeric","encrypted_content":0},
			{"type":"message","id":"msg_valid","role":"assistant","content":"done"}
		]
	}`)
	originalInput := append(json.RawMessage(nil), original.Input...)

	attempt, err := PrepareResponsesRequestAttempt(original, dto.ChannelSettings{}, false)
	require.NoError(t, err)
	require.NotNil(t, attempt.Compatibility)
	assert.Equal(t, 3, attempt.Compatibility.DroppedNonReplayableReasoningItems)
	assert.Equal(t, 4, attempt.Compatibility.NormalizedRequestItemIDs)
	require.NotNil(t, attempt.Degradation)
	assert.Equal(t, hosttypes.RequestDegradationReasonNonReplayableReasoning, attempt.Degradation.Reason)
	assert.Equal(t, 3, attempt.Degradation.DroppedReasoningItems)

	items := responsesAttemptItems(t, attempt.Request)
	require.Len(t, items, 6)
	assert.Equal(t, "msg_message", items[0]["id"])
	assert.Equal(t, true, items[0]["vendor_field"])
	assert.Equal(t, "fc_function", items[1]["id"])
	assert.Equal(t, "call_1", items[1]["call_id"])
	assert.Equal(t, "item_output", items[2]["id"])
	assert.Equal(t, "call_1", items[2]["call_id"])
	assert.Equal(t, "rs_valid", items[3]["id"])
	assert.Equal(t, "rs_numeric", items[4]["id"])
	assert.Equal(t, "msg_valid", items[5]["id"])
	assert.Equal(t, originalInput, original.Input)
}

func TestPrepareResponsesRequestAttemptAllowsSummaryOnlyReasoningWithoutDisablingIDFix(t *testing.T) {
	original := responsesRequestFromJSON(t, `{
		"model":"gpt-test",
		"store":false,
		"input":[
			{"type":"reasoning","id":"item_reasoning","summary":[],"vendor_field":"kept"},
			{"type":"function_call","id":"item_function","call_id":"call_1","name":"lookup","arguments":"{}"}
		]
	}`)
	allow := true

	attempt, err := PrepareResponsesRequestAttempt(original, dto.ChannelSettings{
		AllowReasoningWithoutEncryptedContent: &allow,
	}, false)
	require.NoError(t, err)
	require.NotNil(t, attempt.Compatibility)
	assert.Zero(t, attempt.Compatibility.DroppedNonReplayableReasoningItems)
	assert.Equal(t, 2, attempt.Compatibility.NormalizedRequestItemIDs)
	assert.Nil(t, attempt.Degradation)

	items := responsesAttemptItems(t, attempt.Request)
	require.Len(t, items, 2)
	assert.Equal(t, "rs_reasoning", items[0]["id"])
	assert.Equal(t, "kept", items[0]["vendor_field"])
	assert.Equal(t, "fc_function", items[1]["id"])
	assert.Equal(t, "call_1", items[1]["call_id"])
}

func TestPrepareResponsesRequestAttemptUsesDeterministicCollisionFreeIDs(t *testing.T) {
	original := responsesRequestFromJSON(t, `{
		"model":"gpt-test",
		"store":false,
		"input":[
			{"type":"function_call","id":"fc_collision","call_id":"call_1"},
			{"type":"function_call","id":"item_collision","call_id":"call_2"},
			{"type":"function_call","id":"item_collision","call_id":"call_3"}
		]
	}`)

	first, err := PrepareResponsesRequestAttempt(original, dto.ChannelSettings{}, false)
	require.NoError(t, err)
	second, err := PrepareResponsesRequestAttempt(original, dto.ChannelSettings{}, false)
	require.NoError(t, err)
	firstItems := responsesAttemptItems(t, first.Request)
	secondItems := responsesAttemptItems(t, second.Request)

	assert.Equal(t, "fc_collision", firstItems[0]["id"])
	assert.NotEqual(t, "fc_collision", firstItems[1]["id"])
	assert.Equal(t, firstItems[1]["id"], firstItems[2]["id"])
	assert.Equal(t, firstItems[1]["id"], secondItems[1]["id"])
	assert.Equal(t, 1, first.Compatibility.NormalizedRequestItemIDs)
}

func TestPrepareResponsesRequestAttemptDoesNotPolluteCrossChannelRetries(t *testing.T) {
	original := responsesRequestFromJSON(t, `{
		"model":"gpt-test",
		"store":false,
		"input":[
			{"type":"reasoning","id":"item_reasoning","summary":[]},
			{"type":"function_call","id":"item_function","call_id":"call_1"}
		]
	}`)
	disabled := false
	fixDisabled := dto.ChannelSettings{ResponsesCompatibilityFix: &disabled}

	for _, order := range [][]dto.ChannelSettings{{{}, fixDisabled}, {fixDisabled, {}}} {
		first, err := PrepareResponsesRequestAttempt(original, order[0], false)
		require.NoError(t, err)
		second, err := PrepareResponsesRequestAttempt(original, order[1], false)
		require.NoError(t, err)

		if order[0].ResponsesCompatibilityFixEnabled() {
			assert.Len(t, responsesAttemptItems(t, first.Request), 1)
			assert.NotNil(t, first.Compatibility)
		} else {
			assert.Len(t, responsesAttemptItems(t, first.Request), 2)
			assert.Nil(t, first.Compatibility)
		}
		if order[1].ResponsesCompatibilityFixEnabled() {
			assert.Len(t, responsesAttemptItems(t, second.Request), 1)
			assert.NotNil(t, second.Compatibility)
		} else {
			assert.Len(t, responsesAttemptItems(t, second.Request), 2)
			assert.Nil(t, second.Compatibility)
		}
		assert.Len(t, responsesAttemptItems(t, original), 2)
	}
}

func TestPrepareResponsesRequestAttemptKeepsOutOfScopeAndDisabledRequestsUnchanged(t *testing.T) {
	disabled := false
	testCases := []struct {
		name        string
		body        string
		settings    dto.ChannelSettings
		passThrough bool
	}{
		{
			name:     "main compatibility disabled",
			body:     `{"model":"gpt-test","store":false,"input":[{"type":"reasoning","id":"item_1"}]}`,
			settings: dto.ChannelSettings{ResponsesCompatibilityFix: &disabled},
		},
		{
			name:        "request body passthrough",
			body:        `{"model":"gpt-test","store":false,"input":[{"type":"reasoning","id":"item_1"}]}`,
			passThrough: true,
		},
		{
			name: "store omitted",
			body: `{"model":"gpt-test","input":[{"type":"reasoning","id":"item_1"}]}`,
		},
		{
			name: "store true",
			body: `{"model":"gpt-test","store":true,"input":[{"type":"function_call","id":"item_1"}]}`,
		},
		{
			name: "non-array input",
			body: `{"model":"gpt-test","store":false,"input":"hello"}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			original := responsesRequestFromJSON(t, testCase.body)
			originalInput := append(json.RawMessage(nil), original.Input...)

			attempt, err := PrepareResponsesRequestAttempt(original, testCase.settings, testCase.passThrough)
			require.NoError(t, err)
			assert.Nil(t, attempt.Compatibility)
			assert.Nil(t, attempt.Degradation)
			assert.Equal(t, originalInput, attempt.Request.Input)
			assert.Equal(t, originalInput, original.Input)
			assert.NotSame(t, original, attempt.Request)
		})
	}
}
