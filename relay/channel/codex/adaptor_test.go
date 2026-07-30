package codex

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLAlphaSearch(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelBaseUrl: "https://chatgpt.com",
		},
		RelayMode: relayconstant.RelayModeAlphaSearch,
	}

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://chatgpt.com/backend-api/codex/alpha/search", url)
}

func TestConvertOpenAIResponsesRequestBoundsChannelTestOutputWithCodexTextFormat(t *testing.T) {
	maxOutputTokens := uint(16)
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, &relaycommon.RelayInfo{
		IsChannelTest: true,
		RelayMode:     relayconstant.RelayModeResponses,
		ChannelMeta:   &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model:           "gpt-test",
		Input:           []byte(`[{"role":"user","content":"hi"}]`),
		MaxOutputTokens: &maxOutputTokens,
	})
	require.NoError(t, err)

	request, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.Nil(t, request.MaxOutputTokens)

	var text struct {
		Verbosity string `json:"verbosity"`
		Format    struct {
			Type   string `json:"type"`
			Name   string `json:"name"`
			Strict bool   `json:"strict"`
			Schema struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"schema"`
		} `json:"format"`
	}
	require.NoError(t, common.Unmarshal(request.Text, &text))
	assert.Equal(t, "low", text.Verbosity)
	assert.Equal(t, "json_schema", text.Format.Type)
	assert.Equal(t, "channel_test", text.Format.Name)
	assert.True(t, text.Format.Strict)
	assert.Equal(t, []string{"ok"}, text.Format.Schema.Properties["status"].Enum)

	upstreamBody, err := common.Marshal(request)
	require.NoError(t, err)
	assert.NotContains(t, string(upstreamBody), "max_output_tokens")
	assert.Contains(t, string(upstreamBody), `"verbosity":"low"`)
	assert.Contains(t, string(upstreamBody), `"enum":["ok"]`)
}
