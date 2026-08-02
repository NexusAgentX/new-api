package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrependFailureSampleNoncePreservesUnknownFieldsAndSample(t *testing.T) {
	original := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}],"vendor_extension":{"keep":true}}`)

	first, err := PrependFailureSampleNonce(original, types.RelayFormatOpenAI, "nonce-one")
	require.NoError(t, err)
	second, err := PrependFailureSampleNonce(original, types.RelayFormatOpenAI, "nonce-two")
	require.NoError(t, err)

	assert.Equal(t, string(original), `{"model":"gpt-test","messages":[{"role":"user","content":"hello"}],"vendor_extension":{"keep":true}}`)
	assert.Contains(t, string(first), "nonce-one hello")
	assert.Contains(t, string(second), "nonce-two hello")
	assert.NotContains(t, string(first), "nonce-two")
	assert.Contains(t, string(first), `"vendor_extension":{"keep":true}`)
}

func TestPrependFailureSampleNonceUsesSafeTextFieldsAcrossFormats(t *testing.T) {
	tests := []struct {
		name   string
		format types.RelayFormat
		body   string
		nonce  string
		want   string
	}{
		{
			name:   "responses input message",
			format: types.RelayFormatOpenAIResponses,
			body:   `{"input":[{"type":"reasoning","summary":[]},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`,
			nonce:  "nonce-responses",
			want:   "nonce-responses hello",
		},
		{
			name:   "claude text block",
			format: types.RelayFormatClaude,
			body:   `{"model":"claude","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`,
			nonce:  "nonce-claude",
			want:   "nonce-claude hello",
		},
		{
			name:   "gemini user part",
			format: types.RelayFormatGemini,
			body:   `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`,
			nonce:  "nonce-gemini",
			want:   "nonce-gemini hello",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updated, err := PrependFailureSampleNonce([]byte(test.body), test.format, test.nonce)
			require.NoError(t, err)
			assert.Contains(t, string(updated), test.want)
			assert.NotEqual(t, test.body, string(updated))
		})
	}
}

func TestPrependFailureSampleNonceFallsBackWithoutSafeText(t *testing.T) {
	_, err := PrependFailureSampleNonce([]byte(`{"messages":[{"role":"tool","content":"result"}]}`), types.RelayFormatOpenAI, "nonce")
	require.ErrorIs(t, err, ErrNoSafeFailureSampleText)
}
