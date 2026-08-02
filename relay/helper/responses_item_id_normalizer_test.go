package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResponsesResponseJSONPreservesUnknownFieldsAndCallIDs(t *testing.T) {
	input := []byte(`{
		"id":"resp_1",
		"object":"response",
		"vendor_response":{"keep":true},
		"output":[
			{"type":"function_call","id":"item_function","call_id":"call_1","name":"lookup","arguments":"{}","vendor_item":1},
			{"type":"message","id":"item_message","role":"assistant","content":[],"vendor_item":2},
			{"type":"reasoning","id":"item_reasoning","summary":[],"vendor_item":3},
			{"type":"function_call","id":"fc_valid","call_id":"call_2"},
			{"type":"function_call_output","id":"item_output","call_id":"call_1","output":"ok"}
		]
	}`)

	normalized, count, err := NormalizeResponsesResponseJSON(input, NewResponsesItemIDNormalizer())
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	var response map[string]any
	require.NoError(t, common.Unmarshal(normalized, &response))
	assert.Equal(t, map[string]any{"keep": true}, response["vendor_response"])
	output := response["output"].([]any)
	functionCall := output[0].(map[string]any)
	message := output[1].(map[string]any)
	reasoning := output[2].(map[string]any)
	validFunctionCall := output[3].(map[string]any)
	functionOutput := output[4].(map[string]any)
	assert.Equal(t, "fc_function", functionCall["id"])
	assert.Equal(t, "call_1", functionCall["call_id"])
	assert.Equal(t, float64(1), functionCall["vendor_item"])
	assert.Equal(t, "msg_message", message["id"])
	assert.Equal(t, "rs_reasoning", reasoning["id"])
	assert.NotContains(t, reasoning, "encrypted_content")
	assert.Equal(t, "fc_valid", validFunctionCall["id"])
	assert.Equal(t, "item_output", functionOutput["id"])
	assert.Equal(t, "call_1", functionOutput["call_id"])
}

func TestNormalizeResponsesStreamEventJSONKeepsOneStableMappingAcrossEventChain(t *testing.T) {
	normalizer := NewResponsesItemIDNormalizer()
	events := []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item_function","call_id":"call_1","vendor_item":"added"},"vendor_event":1}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"item_function","delta":"{}","vendor_event":2}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"item_function","call_id":"call_1","arguments":"{}"},"vendor_event":3}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"function_call","id":"item_function","call_id":"call_1","arguments":"{}","vendor_final":true}]},"vendor_event":4}`,
	}

	for index, event := range events {
		normalized, count, err := NormalizeResponsesStreamEventJSON([]byte(event), normalizer)
		require.NoError(t, err)
		if index == 0 {
			assert.Equal(t, 1, count)
		} else {
			assert.Zero(t, count)
		}
		assert.NotContains(t, string(normalized), `"item_function"`)
		assert.Contains(t, string(normalized), `"fc_function"`)
		assert.Contains(t, string(normalized), `"vendor_event":`)
		if index != 1 {
			assert.Contains(t, string(normalized), `"call_id":"call_1"`)
		}
	}
	assert.Equal(t, 1, normalizer.NormalizedCount())
}

func TestNormalizeResponsesStreamEventJSONInfersDeltaItemTypeWithoutAddedEvent(t *testing.T) {
	tests := []struct {
		event string
		want  string
	}{
		{
			event: `{"type":"response.function_call_arguments.delta","item_id":"item_function","delta":"{}"}`,
			want:  `"item_id":"fc_function"`,
		},
		{
			event: `{"type":"response.reasoning_summary_text.delta","item_id":"item_reasoning","delta":"summary"}`,
			want:  `"item_id":"rs_reasoning"`,
		},
		{
			event: `{"type":"response.output_text.delta","item_id":"item_message","delta":"hello"}`,
			want:  `"item_id":"msg_message"`,
		},
	}

	for _, test := range tests {
		normalized, count, err := NormalizeResponsesStreamEventJSON(
			[]byte(test.event),
			NewResponsesItemIDNormalizer(),
		)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.Contains(t, string(normalized), test.want)
	}
}

func TestNormalizeResponsesResponseJSONKeepsCompliantDocumentByteIdentical(t *testing.T) {
	input := []byte(`{"id":"resp_1","output":[{"type":"function_call","id":"fc_1"},{"type":"message","id":"msg_1"},{"type":"reasoning","id":"rs_1"}]}`)

	normalized, count, err := NormalizeResponsesResponseJSON(input, NewResponsesItemIDNormalizer())
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.Equal(t, input, normalized)
}
