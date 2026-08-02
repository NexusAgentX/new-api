package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildChannelFailureSampleUsesPreAttemptBodyWithoutHeaders(t *testing.T) {
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	originalSetting := *setting
	setting.FailureSampleReplayEnabled = true
	setting.FailureSampleMaxMB = 4
	t.Cleanup(func() { *setting = originalSetting })

	body := `{"model":"gpt-test","store":false,"input":[{"type":"reasoning","id":"bad-reasoning","summary":[]},{"type":"message","id":"bad-message","role":"user","content":[{"type":"input_text","text":"production prompt"}]}]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer client-secret")
	c.Set(common.RequestIdKey, "request-123")
	defer common.CleanupBodyStorage(c)

	request, err := helper.GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
	require.NoError(t, err)
	responsesRequest, ok := request.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	attempt, err := helper.PrepareResponsesRequestAttempt(responsesRequest, dto.ChannelSettings{}, false)
	require.NoError(t, err)
	assert.NotEqual(t, string(responsesRequest.Input), string(attempt.Request.Input))

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-test",
		IsStream:        true,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{
			FailureSampleReplayEnabled: true,
		}},
	}
	sample := buildChannelFailureSample(c, info)
	require.NotNil(t, sample)
	assert.Equal(t, body, string(sample.RequestBody))
	assert.Equal(t, int64(len(body)), sample.BodySize)
	assert.Equal(t, "request-123", sample.RequestId)
	assert.NotContains(t, string(sample.RequestBody), "client-secret")

	metadata, err := common.Marshal(sample)
	require.NoError(t, err)
	assert.NotContains(t, string(metadata), "production prompt")
	assert.NotContains(t, string(metadata), "client-secret")
}

func TestBuildChannelFailureSampleRequiresBothOptInSwitches(t *testing.T) {
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	originalSetting := *setting
	setting.FailureSampleMaxMB = 4
	t.Cleanup(func() { *setting = originalSetting })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`))
	defer common.CleanupBodyStorage(c)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-test",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{
			FailureSampleReplayEnabled: true,
		}},
	}

	setting.FailureSampleReplayEnabled = false
	assert.Nil(t, buildChannelFailureSample(c, info))
	setting.FailureSampleReplayEnabled = true
	info.ChannelSetting.FailureSampleReplayEnabled = false
	assert.Nil(t, buildChannelFailureSample(c, info))
}
