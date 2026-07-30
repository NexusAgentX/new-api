package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relayopenai "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateChannelProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", proxy: "http://proxy.example:8080"},
		{name: "https", proxy: "https://proxy.example:8443"},
		{name: "socks5", proxy: "socks5://proxy.example"},
		{name: "socks5h", proxy: "socks5h://proxy.example:1080/"},
		{name: "unsupported", proxy: "ftp://proxy.example", wantErr: true},
		{name: "path", proxy: "socks5://proxy.example:1080/path", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting, err := common.Marshal(dto.ChannelSettings{Proxy: test.proxy})
			require.NoError(t, err)
			channel := &model.Channel{
				Type:    constant.ChannelTypeOpenAI,
				Setting: common.GetPointer(string(setting)),
			}

			err = validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "invalid channel proxy")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCopyChannelRejectsInvalidLegacyProxySettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	settingBytes, err := common.Marshal(dto.ChannelSettings{
		Proxy: "socks5://proxy.example/legacy-path",
	})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "legacy proxy channel",
		Key:     "test-key",
		Models:  "gpt-test",
		Group:   "default",
		Setting: &setting,
	}
	require.NoError(t, db.Create(origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)

	CopyChannel(ctx)

	assert.Contains(t, recorder.Body.String(), "invalid channel settings")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)
}

func TestDeleteChannelResetsProxyCacheWhenPreReadFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	service.ResetProxyClientCache()
	t.Cleanup(service.ResetProxyClientCache)

	proxyURL := "http://proxy.example:8080"
	beforeDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/999999", nil)

	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	afterDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)
	assert.NotSame(t, beforeDelete, afterDelete)
}

func TestDeleteChannelBatchReportsAndAuditsActualDeletedCount(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	channel := &model.Channel{Name: "existing", Key: "test-key"}
	require.NoError(t, db.Create(channel).Error)

	requestBody, err := common.Marshal(ChannelBatch{Ids: []int{channel.Id, 999999}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DeleteChannelBatch(ctx)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, int64(1), response.Data)

	var auditLog model.Log
	require.NoError(t, db.Order("id desc").First(&auditLog).Error)
	var auditData struct {
		Operation struct {
			Params map[string]any `json:"params"`
		} `json:"op"`
	}
	require.NoError(t, common.UnmarshalJsonStr(auditLog.Other, &auditData))
	assert.Equal(t, float64(1), auditData.Operation.Params["count"])
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}

func TestResolveChannelTestRequestOptionsFromSettings(t *testing.T) {
	stream := true
	sampleTokens := 128
	prependNonce := true
	threshold := 120.0
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetSetting(dto.ChannelSettings{
		TestEndpointType:            string(constant.EndpointTypeOpenAIResponse),
		TestStream:                  &stream,
		TestSampleTokens:            &sampleTokens,
		TestPrependNonce:            &prependNonce,
		TestDisableThresholdSeconds: &threshold,
	})

	options := resolveChannelTestRequestOptionsFromSettings(channel, "gpt-test", true)

	require.Equal(t, string(constant.EndpointTypeOpenAIResponse), options.endpointType)
	require.True(t, options.isStream)
	require.Equal(t, 128, options.sampleTokens)
	require.True(t, options.prependNonce)
	require.True(t, options.monitorFirstResponse)

	manualOptions := resolveChannelTestRequestOptionsFromSettings(channel, "gpt-test", false)
	require.True(t, manualOptions.isStream)
	require.False(t, manualOptions.monitorFirstResponse)

	legacyCodex := &model.Channel{Type: constant.ChannelTypeCodex, Models: "gpt-test"}
	legacyOptions := resolveChannelTestRequestOptionsFromSettings(legacyCodex, "gpt-test", true)
	require.Equal(t, string(constant.EndpointTypeOpenAIResponse), legacyOptions.endpointType)
	require.True(t, legacyOptions.isStream)
	require.True(t, legacyOptions.monitorFirstResponse)
}

func TestParseChannelTestRequestUsesChannelSettingsOnlyWithoutOverrides(t *testing.T) {
	recorder := httptest.NewRecorder()
	contextWithDefaults, _ := gin.CreateTestContext(recorder)
	contextWithDefaults.Request = httptest.NewRequest(http.MethodGet, "/api/channel/test/1", nil)

	_, defaultOptions, err := parseChannelTestRequest(contextWithDefaults)
	require.NoError(t, err)
	require.True(t, defaultOptions.useChannelSettings)
	require.False(t, defaultOptions.monitorFirstResponse)

	contextWithOverride, _ := gin.CreateTestContext(recorder)
	contextWithOverride.Request = httptest.NewRequest(http.MethodGet, "/api/channel/test/1?model=gpt-test", nil)

	modelName, overrideOptions, err := parseChannelTestRequest(contextWithOverride)
	require.NoError(t, err)
	require.Equal(t, "gpt-test", modelName)
	require.False(t, overrideOptions.useChannelSettings)
}

func TestNormalizeChannelTestEndpointKeepsCodexResponsesPriority(t *testing.T) {
	codexChannel := &model.Channel{Type: constant.ChannelTypeCodex}

	tests := []struct {
		name      string
		modelName string
		expected  string
	}{
		{name: "embedding-like model", modelName: "text-embedding-3-small", expected: string(constant.EndpointTypeOpenAIResponse)},
		{name: "rerank-like model", modelName: "vendor-rerank-model", expected: string(constant.EndpointTypeOpenAIResponse)},
		{name: "compact model", modelName: "gpt-test" + ratio_setting.CompactModelSuffix, expected: string(constant.EndpointTypeOpenAIResponseCompact)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, normalizeChannelTestEndpoint(codexChannel, test.modelName, dto.ChannelTestEndpointAuto))
		})
	}
}

func TestBuildTestRequestPreservesLegacyZeroSamplePrompts(t *testing.T) {
	chatRequest, err := buildTestRequest(
		"gpt-test",
		string(constant.EndpointTypeOpenAI),
		&model.Channel{},
		channelTestRequestOptions{},
	)
	require.NoError(t, err)
	chat, ok := chatRequest.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chat.Messages, 1)
	assert.Equal(t, "hi", chat.Messages[0].StringContent())

	embeddingRequest, err := buildTestRequest(
		"embedding-test",
		string(constant.EndpointTypeEmbeddings),
		&model.Channel{},
		channelTestRequestOptions{sampleTokens: 0},
	)
	require.NoError(t, err)
	embedding, ok := embeddingRequest.(*dto.EmbeddingRequest)
	require.True(t, ok)
	assert.Equal(t, []any{"hello world"}, embedding.Input)

	rerankRequest, err := buildTestRequest(
		"rerank-test",
		string(constant.EndpointTypeJinaRerank),
		&model.Channel{},
		channelTestRequestOptions{},
	)
	require.NoError(t, err)
	rerank, ok := rerankRequest.(*dto.RerankRequest)
	require.True(t, ok)
	assert.Equal(t, "What is Deep Learning?", rerank.Query)

	expandedRequest, err := buildTestRequest(
		"embedding-test",
		string(constant.EndpointTypeEmbeddings),
		&model.Channel{},
		channelTestRequestOptions{sampleTokens: 3},
	)
	require.NoError(t, err)
	expanded, ok := expandedRequest.(*dto.EmbeddingRequest)
	require.True(t, ok)
	expandedInput, ok := expanded.Input.([]any)
	require.True(t, ok)
	require.Len(t, expandedInput, 1)
	assert.Contains(t, expandedInput[0], "sample sample sample")
}

func TestBuildTestRequestUsesResponsesStreamAndNonceSample(t *testing.T) {
	request, err := buildTestRequest(
		"gpt-test",
		string(constant.EndpointTypeOpenAIResponse),
		&model.Channel{},
		channelTestRequestOptions{
			isStream:     true,
			sampleTokens: 4,
			prependNonce: true,
		},
	)
	require.NoError(t, err)

	responseRequest, ok := request.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.True(t, *responseRequest.Stream)
	require.Equal(t, uint(16), *responseRequest.MaxOutputTokens)

	var input []struct {
		Content string `json:"content"`
	}
	require.NoError(t, common.Unmarshal(responseRequest.Input, &input))
	require.Len(t, input, 1)
	assert.Contains(t, input[0].Content, "nonce-")
	assert.Contains(t, input[0].Content, "sample sample sample sample")

	secondRequest, err := buildTestRequest(
		"gpt-test",
		string(constant.EndpointTypeOpenAIResponse),
		&model.Channel{},
		channelTestRequestOptions{
			isStream:     true,
			sampleTokens: 4,
			prependNonce: true,
		},
	)
	require.NoError(t, err)
	secondResponseRequest, ok := secondRequest.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	var secondInput []struct {
		Content string `json:"content"`
	}
	require.NoError(t, common.Unmarshal(secondResponseRequest.Input, &secondInput))
	require.Len(t, secondInput, 1)
	assert.NotEqual(t, input[0].Content, secondInput[0].Content)
}

func TestBuildTestRequestKeepsImageAndCompactSamplesSmall(t *testing.T) {
	imageRequest, err := buildTestRequest(
		"image-test",
		string(constant.EndpointTypeImageGeneration),
		&model.Channel{},
		channelTestRequestOptions{sampleTokens: 8192, prependNonce: true},
	)
	require.NoError(t, err)
	image, ok := imageRequest.(*dto.ImageRequest)
	require.True(t, ok)
	assert.Contains(t, image.Prompt, "nonce-")
	assert.Contains(t, image.Prompt, "a cute cat")
	assert.NotContains(t, image.Prompt, "sample sample")

	compactRequest, err := buildTestRequest(
		"gpt-test",
		string(constant.EndpointTypeOpenAIResponseCompact),
		&model.Channel{},
		channelTestRequestOptions{sampleTokens: 8192, prependNonce: true},
	)
	require.NoError(t, err)
	compact, ok := compactRequest.(*dto.OpenAIResponsesCompactionRequest)
	require.True(t, ok)
	assert.Contains(t, string(compact.Input), "nonce-")
	assert.NotContains(t, string(compact.Input), "sample sample")
}

func TestValidateStreamTestResponseBodyRequiresEveryEventToBeValid(t *testing.T) {
	validBody := []byte("data: {\"type\":\"response.created\"}\n\ndata: [DONE]\n")
	require.NoError(t, validateStreamTestResponseBody(validBody))
	require.NoError(t, validateStreamTestResponseBody([]byte(strings.Join([]string{
		": keepalive",
		"id: response-1",
		"retry: 1000",
		"event: message",
		`data: {"type":`,
		`data: "response.created"}`,
		"",
	}, "\n"))))
	require.Error(t, validateStreamTestResponseBody([]byte(`{"type":"response.created"}`)))
	require.Error(t, validateStreamTestResponseBody([]byte("data: malformed\n")))
	require.Error(t, validateStreamTestResponseBody([]byte("data: {\"type\":\"error\",\"error\":{\"message\":\"rate limited\"}}\n")))
	require.Error(t, validateStreamTestResponseBody([]byte("data: {\"type\":\"response.created\"}\n\ndata: {\"type\":\"response.failed\"}\n")))
	require.Error(t, validateStreamTestResponseBody([]byte("data: {\"type\":\"response.created\"}\n\ndata: malformed\n")))
	require.Error(t, validateStreamTestResponseBody([]byte("event: error\ndata: {\"type\":\"response.created\"}\n\n")))
	require.Error(t, validateStreamTestResponseBody([]byte("data: ping\n\ndata: [DONE]\n\n")))

	streamStatus := relaycommon.NewStreamStatus()
	streamStatus.RecordError("invalid or error SSE event")
	require.Error(t, validateTestResponseBody(&relaycommon.RelayInfo{StreamStatus: streamStatus}, validBody, true))

	for _, reason := range []relaycommon.StreamEndReason{
		relaycommon.StreamEndReasonTimeout,
		relaycommon.StreamEndReasonScannerErr,
		relaycommon.StreamEndReasonPanic,
		relaycommon.StreamEndReasonPingFail,
	} {
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(reason, fmt.Errorf("stream failed"))
		err := validateTestResponseBody(&relaycommon.RelayInfo{StreamStatus: status}, validBody, true)
		require.ErrorContains(t, err, string(reason))
	}

	for _, reason := range []relaycommon.StreamEndReason{
		relaycommon.StreamEndReasonDone,
		relaycommon.StreamEndReasonEOF,
	} {
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(reason, nil)
		require.NoError(t, validateTestResponseBody(&relaycommon.RelayInfo{StreamStatus: status}, validBody, true))
	}
}

func TestResponsesFailureAfterValidEventCannotRecoverChannel(t *testing.T) {
	originalStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalStreamingTimeout })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stream := true
	info := relaycommon.GenRelayInfoResponses(ctx, &dto.OpenAIResponsesRequest{Stream: &stream})
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}
	info.DisablePing = true
	info.RequireValidFirstResponseEvent = true
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`data: {"type":"response.failed","response":{"status":"failed","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"retry later"}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, streamErr := relayopenai.OaiResponsesStreamHandler(ctx, info, resp)

	require.Nil(t, streamErr)
	require.NotNil(t, usage)
	require.NotNil(t, info.StreamStatus)
	require.True(t, info.StreamStatus.HasErrors())
	validationErr := validateTestResponseBody(info, recorder.Body.Bytes(), true)
	require.Error(t, validationErr)
	require.False(t, shouldRecoverChannelAfterTest(
		testResult{localErr: validationErr},
		nil,
		common.ChannelStatusAutoDisabled,
	))
}

func TestEffectiveChannelTestDisableThreshold(t *testing.T) {
	original := common.ChannelDisableThreshold
	common.ChannelDisableThreshold = 5
	t.Cleanup(func() { common.ChannelDisableThreshold = original })

	channel := &model.Channel{}
	threshold, enabled := effectiveChannelTestDisableThreshold(channel)
	require.True(t, enabled)
	require.Equal(t, 5.0, threshold)

	require.Nil(t, channelTestDurationError(channel, 5000))
	require.NotNil(t, channelTestDurationError(channel, 5001))

	disabled := 0.0
	channel.SetSetting(dto.ChannelSettings{TestDisableThresholdSeconds: &disabled})
	_, enabled = effectiveChannelTestDisableThreshold(channel)
	require.False(t, enabled)
	require.Nil(t, channelTestDurationError(channel, 600_000))

	override := 120.0
	channel.SetSetting(dto.ChannelSettings{TestDisableThresholdSeconds: &override})
	threshold, enabled = effectiveChannelTestDisableThreshold(channel)
	require.True(t, enabled)
	require.Equal(t, 120.0, threshold)
	require.Nil(t, channelTestDurationError(channel, 120_000))
	require.NotNil(t, channelTestDurationError(channel, 120_001))
}

func TestEvaluateAutomaticChannelTestResultAppliesDurationThresholdWithoutAutoDisable(t *testing.T) {
	originalDisable := common.AutomaticDisableChannelEnabled
	originalEnable := common.AutomaticEnableChannelEnabled
	common.AutomaticDisableChannelEnabled = false
	common.AutomaticEnableChannelEnabled = true
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = originalDisable
		common.AutomaticEnableChannelEnabled = originalEnable
	})

	threshold := 1.0
	channel := &model.Channel{Status: common.ChannelStatusAutoDisabled}
	channel.SetSetting(dto.ChannelSettings{TestDisableThresholdSeconds: &threshold})

	newAPIError, shouldBan := evaluateAutomaticChannelTestResult(channel, testResult{}, 1001)
	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, newAPIError.GetErrorCode())
	require.False(t, shouldBan)
	require.False(t, shouldRecoverChannelAfterTest(testResult{}, newAPIError, channel.Status))

	disabledThreshold := 0.0
	channel.SetSetting(dto.ChannelSettings{TestDisableThresholdSeconds: &disabledThreshold})
	newAPIError, shouldBan = evaluateAutomaticChannelTestResult(channel, testResult{}, 1001)
	require.Nil(t, newAPIError)
	require.False(t, shouldBan)
}

func TestShouldRecoverChannelAfterTestRejectsObservedFirstResponseTimeout(t *testing.T) {
	original := common.AutomaticEnableChannelEnabled
	common.AutomaticEnableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticEnableChannelEnabled = original })

	require.True(t, shouldRecoverChannelAfterTest(
		testResult{},
		nil,
		common.ChannelStatusAutoDisabled,
	))
	require.False(t, shouldRecoverChannelAfterTest(
		testResult{firstResponseTimedOut: true},
		nil,
		common.ChannelStatusAutoDisabled,
	))
	require.False(t, shouldRecoverChannelAfterTest(
		testResult{localErr: fmt.Errorf("invalid SSE")},
		nil,
		common.ChannelStatusAutoDisabled,
	))
	rateLimitError := types.NewOpenAIError(
		fmt.Errorf("rate limited"),
		types.ErrorCodeBadResponse,
		http.StatusTooManyRequests,
	)
	require.False(t, shouldRecoverChannelAfterTest(
		testResult{newAPIError: rateLimitError},
		rateLimitError,
		common.ChannelStatusAutoDisabled,
	))
}

func TestPerformChannelTestsRecoversAfterNormalMonitoredResponsesStream(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	originalAutomaticEnable := common.AutomaticEnableChannelEnabled
	originalAutomaticDisable := common.AutomaticDisableChannelEnabled
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRequestInterval := common.RequestInterval
	originalStreamingTimeout := constant.StreamingTimeout
	firstResponseSetting := operation_setting.GetFirstResponseTimeoutSetting()
	originalFirstResponseSetting := *firstResponseSetting
	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":1}`))
	common.AutomaticEnableChannelEnabled = true
	common.AutomaticDisableChannelEnabled = false
	common.MemoryCacheEnabled = false
	common.RequestInterval = 0
	constant.StreamingTimeout = 30
	firstResponseSetting.RetryEnabled = false
	firstResponseSetting.DisableEnabled = false
	firstResponseSetting.TimeoutSeconds = 1
	service.InitHttpClient()
	t.Cleanup(func() {
		common.AutomaticEnableChannelEnabled = originalAutomaticEnable
		common.AutomaticDisableChannelEnabled = originalAutomaticDisable
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RequestInterval = originalRequestInterval
		constant.StreamingTimeout = originalStreamingTimeout
		*firstResponseSetting = originalFirstResponseSetting
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/responses", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_test","status":"in_progress"}}`,
			`data: {"type":"response.completed","response":{"id":"resp_test","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			`data: [DONE]`,
			``,
		}, "\n"))
	}))
	defer upstream.Close()

	root := &model.User{
		Username: "channel-test-root",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1_000_000,
	}
	require.NoError(t, db.Create(root).Error)
	stream := true
	disableThreshold := 0.0
	channel := &model.Channel{
		Name:      "responses-recovery-test",
		Type:      constant.ChannelTypeOpenAI,
		Key:       "test-key",
		BaseURL:   common.GetPointer(upstream.URL),
		Models:    "gpt-4o-mini",
		TestModel: common.GetPointer("gpt-4o-mini"),
		Group:     "default",
		Status:    common.ChannelStatusAutoDisabled,
	}
	channel.SetSetting(dto.ChannelSettings{
		TestEndpointType:            string(constant.EndpointTypeOpenAIResponse),
		TestStream:                  &stream,
		TestDisableThresholdSeconds: &disableThreshold,
	})
	require.NoError(t, db.Create(channel).Error)

	summary := performChannelTests(t.Context(), []*model.Channel{channel}, root.Id, false, nil)

	assert.Equal(t, channelTestSummary{Tested: 1, Succeeded: 1, Enabled: 1}, summary)
	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)

	staleSummary := performChannelTests(t.Context(), []*model.Channel{channel}, root.Id, false, nil)
	assert.Equal(t, 1, staleSummary.Tested)
	assert.Equal(t, 1, staleSummary.Succeeded)
	assert.Zero(t, staleSummary.Enabled)
}

func TestStreamResponseValidationIsIndependentFromLogPreviewLimit(t *testing.T) {
	largeContent := strings.Repeat("x", maxStreamTestLogBytes+512)
	healthyBody := fmt.Sprintf("data: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\ndata: [DONE]\n\n", largeContent)

	preview, err := readTestResponseBody(io.NopCloser(strings.NewReader(healthyBody)), true, "")

	require.NoError(t, err)
	assert.LessOrEqual(t, len(preview), maxStreamTestLogBytes)
	assert.Contains(t, string(preview), "[stream log preview truncated]")
	require.NoError(t, validateTestResponseBody(nil, preview, true))
}

func TestStreamResponseValidationRejectsInvalidEventAfterLogPreviewLimit(t *testing.T) {
	validEvent := "data: {\"type\":\"response.created\"}\n\n"
	paddingLength := maxStreamTestLogBytes - len(validEvent) - 2
	require.Positive(t, paddingLength)
	prefix := validEvent + ":" + strings.Repeat("p", paddingLength) + "\n"
	require.Len(t, prefix, maxStreamTestLogBytes)
	body := prefix + "data: malformed\n\n"

	preview, err := readTestResponseBody(io.NopCloser(strings.NewReader(body)), true, "")

	require.ErrorContains(t, err, "invalid or error stream event")
	assert.LessOrEqual(t, len(preview), maxStreamTestLogBytes)
}

func TestStreamResponsePreviewRedactsNonceBeforeTruncation(t *testing.T) {
	nonce := "nonce-abcdefghijklmnopqrstuvwx"
	marker := "\n...[stream log preview truncated]"
	eventPrefix := `data: {"type":"response.output_text.delta","delta":"nonce-ordinary`
	nonceStart := maxStreamTestLogBytes - len(marker) - 16
	paddingLength := nonceStart - len(eventPrefix)
	require.Positive(t, paddingLength)
	body := eventPrefix + strings.Repeat("x", paddingLength) + nonce + strings.Repeat("z", 128) + `"}` + "\n\n"
	require.Equal(t, nonceStart, strings.Index(body, nonce))

	preview, err := readTestResponseBody(io.NopCloser(strings.NewReader(body)), true, nonce)

	require.NoError(t, err)
	previewText := string(preview)
	assert.Len(t, preview, maxStreamTestLogBytes)
	assert.Contains(t, previewText, "nonce-ordinary")
	assert.Contains(t, previewText, common.RedactedSensitiveValue)
	assert.NotContains(t, previewText, nonce)
	assert.NotContains(t, previewText, nonce[:16])
	assert.NotContains(t, previewText, nonce[6:14])
}

func TestStickyProtocolFailureRejectsLaterValidEvent(t *testing.T) {
	info := &relaycommon.RelayInfo{RequireValidFirstResponseEvent: true}
	require.False(t, info.SetFirstResponseTimeFromJSON([]byte("malformed")))
	require.True(t, info.SetFirstResponseTimeFromJSON([]byte(`{"type":"response.created"}`)))

	err := validateTestResponseBody(
		info,
		[]byte("data: {\"type\":\"response.created\"}\n\ndata: [DONE]\n\n"),
		true,
	)

	require.ErrorContains(t, err, "invalid or error upstream event")
}

func TestPerformChannelTestsCountsLocalOnlyErrorAsFailure(t *testing.T) {
	setupModelListControllerTestDB(t)
	originalRequestInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalRequestInterval })

	threshold := 0.0
	channel := &model.Channel{
		Type:   constant.ChannelTypeMidjourney,
		Status: common.ChannelStatusEnabled,
	}
	channel.SetSetting(dto.ChannelSettings{TestDisableThresholdSeconds: &threshold})

	summary := performChannelTests(t.Context(), []*model.Channel{channel}, 0, false, nil)

	assert.Equal(t, channelTestSummary{Tested: 1, Failed: 1}, summary)
}

func TestRedactChannelTestArtifactsCoversWrappedAndStructuredErrors(t *testing.T) {
	nonce := "nonce-abcdefghijklmnopqrstuvwx"
	metadata, err := common.Marshal(map[string]any{
		"echo":     nonce,
		"ordinary": "nonce-ordinary",
	})
	require.NoError(t, err)
	structuredErr := types.WithOpenAIError(types.OpenAIError{
		Message:  "echo " + nonce + " nonce-ordinary",
		Type:     "type-" + nonce,
		Param:    "param-" + nonce,
		Code:     "code-" + nonce,
		Metadata: metadata,
	}, http.StatusBadRequest)
	result := testResult{
		newAPIError: structuredErr,
		localErr:    fmt.Errorf("wrapped %s nonce-ordinary: %w", nonce, structuredErr),
	}
	var body bytes.Buffer
	body.WriteString("response " + nonce + " nonce-ordinary")

	redactChannelTestArtifacts(&result, &body, nonce)

	require.Error(t, result.localErr)
	assert.NotContains(t, result.localErr.Error(), nonce)
	assert.Contains(t, result.localErr.Error(), "nonce-ordinary")
	assert.NotContains(t, result.newAPIError.Error(), nonce)
	assert.NotContains(t, fmt.Sprintf("%+v", result.newAPIError.RelayError), nonce)
	assert.NotContains(t, string(result.newAPIError.Metadata), nonce)
	assert.Contains(t, fmt.Sprintf("%+v", result.newAPIError.RelayError), "nonce-ordinary")
	assert.NotContains(t, body.String(), nonce)
	assert.Contains(t, body.String(), "nonce-ordinary")
}

func TestChannelTestNonceIsRedactedFromSuccessAndErrorBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "successful echo", statusCode: http.StatusOK},
		{name: "error echo", statusCode: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.Log{}))

			originalMemoryCacheEnabled := common.MemoryCacheEnabled
			originalLogConsumeEnabled := common.LogConsumeEnabled
			originalAutomaticDisable := common.AutomaticDisableChannelEnabled
			originalErrorLogEnabled := constant.ErrorLogEnabled
			originalModelRatios := ratio_setting.ModelRatio2JSONString()
			common.MemoryCacheEnabled = false
			common.LogConsumeEnabled = true
			common.AutomaticDisableChannelEnabled = false
			constant.ErrorLogEnabled = true
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":1}`))
			service.InitHttpClient()
			t.Cleanup(func() {
				common.MemoryCacheEnabled = originalMemoryCacheEnabled
				common.LogConsumeEnabled = originalLogConsumeEnabled
				common.AutomaticDisableChannelEnabled = originalAutomaticDisable
				constant.ErrorLogEnabled = originalErrorLogEnabled
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
			})

			var capturedLogs bytes.Buffer
			common.LogWriterMu.Lock()
			originalWriter := gin.DefaultWriter
			originalErrorWriter := gin.DefaultErrorWriter
			gin.DefaultWriter = &capturedLogs
			gin.DefaultErrorWriter = &capturedLogs
			common.LogWriterMu.Unlock()
			t.Cleanup(func() {
				common.LogWriterMu.Lock()
				gin.DefaultWriter = originalWriter
				gin.DefaultErrorWriter = originalErrorWriter
				common.LogWriterMu.Unlock()
			})

			nonceChan := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request dto.GeneralOpenAIRequest
				require.NoError(t, common.DecodeJson(r.Body, &request))
				require.NotEmpty(t, request.Messages)
				content := request.Messages[len(request.Messages)-1].StringContent()
				nonce := strings.SplitN(content, "\n", 2)[0]
				nonceChan <- nonce
				echo := "echo " + content + " nonce-ordinary"
				w.Header().Set("Content-Type", "application/json")
				if test.statusCode != http.StatusOK {
					w.WriteHeader(test.statusCode)
					responseBody, err := common.Marshal(map[string]any{
						"error": map[string]any{"message": echo},
					})
					require.NoError(t, err)
					_, _ = w.Write(responseBody)
					return
				}
				responseBody, err := common.Marshal(dto.OpenAITextResponse{
					Id:      "chatcmpl_nonce_test",
					Object:  "chat.completion",
					Created: 1,
					Model:   "gpt-4o-mini",
					Choices: []dto.OpenAITextResponseChoice{{
						Index:        0,
						Message:      dto.Message{Role: "assistant", Content: echo},
						FinishReason: "stop",
					}},
					Usage: dto.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
				})
				require.NoError(t, err)
				_, _ = w.Write(responseBody)
			}))
			defer upstream.Close()

			root := &model.User{
				Username: "nonce-test-root",
				Role:     common.RoleRootUser,
				Status:   common.UserStatusEnabled,
				Group:    "default",
				Quota:    1_000_000,
			}
			require.NoError(t, db.Create(root).Error)
			autoBan := 0
			channel := &model.Channel{
				Name:      "nonce-echo-test",
				Type:      constant.ChannelTypeOpenAI,
				Key:       "test-key",
				BaseURL:   common.GetPointer(upstream.URL),
				Models:    "gpt-4o-mini",
				TestModel: common.GetPointer("gpt-4o-mini"),
				Group:     "default",
				Status:    common.ChannelStatusEnabled,
				AutoBan:   &autoBan,
			}
			require.NoError(t, db.Create(channel).Error)

			result := testChannel(t.Context(), channel, root.Id, "gpt-4o-mini", channelTestRequestOptions{
				endpointType: string(constant.EndpointTypeOpenAI),
				prependNonce: true,
			})
			nonce := <-nonceChan
			require.True(t, strings.HasPrefix(nonce, "nonce-"))

			if test.statusCode == http.StatusOK {
				require.NoError(t, result.localErr)
				require.Nil(t, result.newAPIError)
				assert.Contains(t, capturedLogs.String(), "nonce-ordinary")
			} else {
				require.Error(t, result.localErr)
				require.NotNil(t, result.newAPIError)
				assert.NotContains(t, result.localErr.Error(), nonce)
				assert.NotContains(t, result.newAPIError.Error(), nonce)
				assert.NotContains(t, fmt.Sprintf("%+v", result.newAPIError.RelayError), nonce)

				processChannelError(
					result.context,
					nil,
					*types.NewChannelError(channel.Id, channel.Type, channel.Name, false, channel.Key, false),
					result.newAPIError,
				)
				service.DisableChannel(
					*types.NewChannelError(channel.Id, channel.Type, channel.Name, false, channel.Key, true),
					result.newAPIError.ErrorWithStatusCode(),
				)
				var storedChannel model.Channel
				require.NoError(t, db.First(&storedChannel, channel.Id).Error)
				storedInfo, err := common.Marshal(storedChannel.GetOtherInfo())
				require.NoError(t, err)
				assert.NotContains(t, string(storedInfo), nonce)
			}

			var storedLogs []model.Log
			require.NoError(t, db.Find(&storedLogs).Error)
			storedLogPayload, err := common.Marshal(storedLogs)
			require.NoError(t, err)
			assert.NotContains(t, string(storedLogPayload), nonce)
			assert.NotContains(t, capturedLogs.String(), nonce)
		})
	}
}
