package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/samber/lo"
	"github.com/tidwall/gjson"

	"github.com/gin-gonic/gin"
)

type testResult struct {
	context               *gin.Context
	localErr              error
	newAPIError           *types.NewAPIError
	firstResponseTimedOut bool
}

type channelTestRequestOptions struct {
	endpointType         string
	isStream             bool
	sampleTokens         int
	prependNonce         bool
	systemPrompt         string
	message              string
	parameters           *dto.ChannelTestParameters
	useChannelSettings   bool
	monitorFirstResponse bool
	nonce                string
}

func normalizeChannelTestEndpoint(channel *model.Channel, modelName, endpointType string) string {
	normalized := strings.TrimSpace(endpointType)
	if normalized != "" && normalized != dto.ChannelTestEndpointAuto {
		return normalized
	}

	modelNameLower := strings.ToLower(modelName)
	if strings.HasSuffix(modelName, ratio_setting.CompactModelSuffix) {
		return string(constant.EndpointTypeOpenAIResponseCompact)
	}
	if channel != nil && channel.Type == constant.ChannelTypeCodex {
		return string(constant.EndpointTypeOpenAIResponse)
	}
	if strings.Contains(modelNameLower, "rerank") {
		return string(constant.EndpointTypeJinaRerank)
	}
	if strings.Contains(modelNameLower, "embedding") ||
		strings.HasPrefix(modelNameLower, "m3e") ||
		strings.Contains(modelNameLower, "bge-") ||
		strings.Contains(modelNameLower, "embed") ||
		(channel != nil && channel.Type == constant.ChannelTypeMokaAI) {
		return string(constant.EndpointTypeEmbeddings)
	}
	if channel != nil && channel.Type == constant.ChannelTypeVolcEngine && strings.Contains(modelNameLower, "seedream") {
		return string(constant.EndpointTypeImageGeneration)
	}
	if strings.Contains(modelNameLower, "codex") {
		return string(constant.EndpointTypeOpenAIResponse)
	}
	return string(constant.EndpointTypeOpenAI)
}

func resolveChannelTestModel(channel *model.Channel, testModel string) string {
	testModel = strings.TrimSpace(testModel)
	if testModel != "" {
		return testModel
	}
	if channel.TestModel != nil && *channel.TestModel != "" {
		return strings.TrimSpace(*channel.TestModel)
	}
	models := channel.GetModels()
	if len(models) > 0 {
		testModel = strings.TrimSpace(models[0])
	}
	if testModel == "" {
		return "gpt-4o-mini"
	}
	return testModel
}

func resolveChannelTestRequestOptionsFromSettings(channel *model.Channel, testModel string, monitorFirstResponse bool) channelTestRequestOptions {
	settings := channel.GetSetting()
	configuredEndpointType := strings.TrimSpace(settings.TestEndpointType)
	if !dto.IsSupportedChannelTestEndpointType(configuredEndpointType) {
		configuredEndpointType = dto.ChannelTestEndpointAuto
	}

	endpointType := normalizeChannelTestEndpoint(channel, testModel, configuredEndpointType)
	isStream := shouldUseStreamForAutomaticChannelTest(channel)
	if settings.TestStream != nil {
		isStream = *settings.TestStream
	}
	if dto.IsChannelTestEndpointStreamIncompatible(endpointType) {
		isStream = false
	}

	sampleTokens := 0
	if settings.TestSampleTokens != nil && *settings.TestSampleTokens >= 0 && *settings.TestSampleTokens <= dto.MaxChannelTestSampleTokens {
		sampleTokens = *settings.TestSampleTokens
	}

	return channelTestRequestOptions{
		endpointType:         endpointType,
		isStream:             isStream,
		sampleTokens:         sampleTokens,
		prependNonce:         settings.TestPrependNonce != nil && *settings.TestPrependNonce,
		monitorFirstResponse: monitorFirstResponse && isStream,
	}
}

func effectiveChannelTestDisableThreshold(channel *model.Channel) (float64, bool) {
	threshold := common.ChannelDisableThreshold
	settings := channel.GetSetting()
	if settings.TestDisableThresholdSeconds != nil {
		configuredThreshold := *settings.TestDisableThresholdSeconds
		if !math.IsNaN(configuredThreshold) && !math.IsInf(configuredThreshold, 0) && configuredThreshold >= 0 {
			threshold = configuredThreshold
		}
	}
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold <= 0 {
		return 0, false
	}
	return threshold, true
}

func channelTestDurationError(channel *model.Channel, milliseconds int64) *types.NewAPIError {
	disableThreshold, enabled := effectiveChannelTestDisableThreshold(channel)
	if !enabled || float64(milliseconds)/1000.0 <= disableThreshold {
		return nil
	}
	err := fmt.Errorf("响应时间 %.2fs 超过阈值 %.2fs", float64(milliseconds)/1000.0, disableThreshold)
	return types.NewOpenAIError(err, types.ErrorCodeChannelResponseTimeExceeded, http.StatusRequestTimeout)
}

func evaluateAutomaticChannelTestResult(channel *model.Channel, result testResult, milliseconds int64) (*types.NewAPIError, bool) {
	newAPIError := result.newAPIError
	shouldBanChannel := newAPIError != nil && service.ShouldDisableChannel(newAPIError)
	if shouldBanChannel {
		return newAPIError, true
	}

	durationErr := channelTestDurationError(channel, milliseconds)
	if durationErr == nil {
		return newAPIError, false
	}
	if newAPIError == nil || common.AutomaticDisableChannelEnabled {
		newAPIError = durationErr
	}
	return newAPIError, common.AutomaticDisableChannelEnabled
}

func shouldRecoverChannelAfterTest(result testResult, newAPIError *types.NewAPIError, status int) bool {
	return result.localErr == nil && !result.firstResponseTimedOut && service.ShouldEnableChannel(newAPIError, status)
}

func resolveChannelTestUserID(c *gin.Context) (int, error) {
	if c != nil {
		if userID := c.GetInt("id"); userID > 0 {
			return userID, nil
		}
	}

	var rootUser model.User
	if err := model.DB.Select("id").Where("role = ?", common.RoleRootUser).First(&rootUser).Error; err != nil {
		return 0, fmt.Errorf("failed to resolve channel test user: %w", err)
	}
	if rootUser.Id == 0 {
		return 0, errors.New("failed to resolve channel test user")
	}
	return rootUser.Id, nil
}

func redactChannelTestArtifacts(result *testResult, body *bytes.Buffer, nonce string) {
	if result == nil || nonce == "" {
		return
	}
	if result.newAPIError != nil {
		result.newAPIError.RedactExactValue(nonce)
	}
	if result.localErr != nil {
		var localNewAPIError *types.NewAPIError
		if errors.As(result.localErr, &localNewAPIError) {
			localNewAPIError.RedactExactValue(nonce)
		}
		result.localErr = errors.New(common.RedactExactValue(result.localErr.Error(), nonce))
	}
	if body != nil {
		redactedBody := common.RedactExactValue(body.String(), nonce)
		body.Reset()
		_, _ = body.WriteString(redactedBody)
	}
}

func parseChannelTestRequest(c *gin.Context) (string, channelTestRequestOptions, error) {
	if c.Request.Method != http.MethodPost {
		query := c.Request.URL.Query()
		isStream, _ := strconv.ParseBool(query.Get("stream"))
		return query.Get("model"), channelTestRequestOptions{
			endpointType:       query.Get("endpoint_type"),
			isStream:           isStream,
			useChannelSettings: !query.Has("model") && !query.Has("endpoint_type") && !query.Has("stream"),
		}, nil
	}

	var request dto.ChannelTestRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		return "", channelTestRequestOptions{}, fmt.Errorf("invalid channel test request: %w", err)
	}
	if err := request.Validate(); err != nil {
		return "", channelTestRequestOptions{}, err
	}

	isStream := request.Stream != nil && *request.Stream
	return request.Model, channelTestRequestOptions{
		endpointType: request.EndpointType,
		isStream:     isStream,
		systemPrompt: request.SystemPrompt,
		message:      request.Message,
		parameters:   request.Parameters,
	}, nil
}

func testChannel(ctx context.Context, channel *model.Channel, testUserID int, testModel string, options channelTestRequestOptions) (result testResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	tik := time.Now()
	var unsupportedTestChannelTypes = []int{
		constant.ChannelTypeMidjourney,
		constant.ChannelTypeMidjourneyPlus,
		constant.ChannelTypeSunoAPI,
		constant.ChannelTypeKling,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeDoubaoVideo,
		constant.ChannelTypeVidu,
	}
	if lo.Contains(unsupportedTestChannelTypes, channel.Type) {
		channelTypeName := constant.GetChannelTypeName(channel.Type)
		return testResult{
			localErr: fmt.Errorf("%s channel test is not supported", channelTypeName),
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	testModel = resolveChannelTestModel(channel, testModel)
	if options.useChannelSettings {
		options = resolveChannelTestRequestOptionsFromSettings(channel, testModel, options.monitorFirstResponse)
	}

	endpointType := normalizeChannelTestEndpoint(channel, testModel, options.endpointType)
	if dto.IsChannelTestEndpointStreamIncompatible(endpointType) {
		options.isStream = false
		options.monitorFirstResponse = false
	}

	requestPath := "/v1/chat/completions"
	if endpointInfo, ok := common.GetDefaultEndpointInfo(constant.EndpointType(endpointType)); ok {
		requestPath = endpointInfo.Path
	}
	if strings.HasPrefix(requestPath, "/v1/responses/compact") {
		testModel = ratio_setting.WithCompactModelSuffix(testModel)
	}

	c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, requestPath, nil)
	if options.prependNonce {
		options.nonce = newChannelTestNonce()
		common.SetContextKey(c, constant.ContextKeyChannelTestNonce, options.nonce)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), string(constant.ContextKeyChannelTestNonce), options.nonce))
		defer func() {
			redactChannelTestArtifacts(&result, w.Body, options.nonce)
		}()
	}

	cache, err := model.GetUserCache(testUserID)
	if err != nil {
		return testResult{
			localErr:    err,
			newAPIError: nil,
		}
	}
	cache.WriteContext(c)
	c.Set("id", testUserID)

	//c.Request.Header.Set("Authorization", "Bearer "+channel.Key)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("channel", channel.Type)
	c.Set("base_url", channel.GetBaseURL())
	group, _ := model.GetUserGroup(testUserID, false)
	c.Set("group", group)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, testModel)
	if newAPIError != nil {
		return testResult{
			context:     c,
			localErr:    newAPIError,
			newAPIError: newAPIError,
		}
	}

	// Determine relay format based on endpoint type or request path
	var relayFormat types.RelayFormat
	if endpointType != "" {
		// 根据指定的端点类型设置 relayFormat
		switch constant.EndpointType(endpointType) {
		case constant.EndpointTypeOpenAI:
			relayFormat = types.RelayFormatOpenAI
		case constant.EndpointTypeOpenAIResponse:
			relayFormat = types.RelayFormatOpenAIResponses
		case constant.EndpointTypeOpenAIResponseCompact:
			relayFormat = types.RelayFormatOpenAIResponsesCompaction
		case constant.EndpointTypeAnthropic:
			relayFormat = types.RelayFormatClaude
		case constant.EndpointTypeGemini:
			relayFormat = types.RelayFormatGemini
		case constant.EndpointTypeJinaRerank:
			relayFormat = types.RelayFormatRerank
		case constant.EndpointTypeImageGeneration:
			relayFormat = types.RelayFormatOpenAIImage
		case constant.EndpointTypeEmbeddings:
			relayFormat = types.RelayFormatEmbedding
		default:
			relayFormat = types.RelayFormatOpenAI
		}
	} else {
		// 根据请求路径自动检测
		relayFormat = types.RelayFormatOpenAI
		if c.Request.URL.Path == "/v1/embeddings" {
			relayFormat = types.RelayFormatEmbedding
		}
		if c.Request.URL.Path == "/v1/images/generations" {
			relayFormat = types.RelayFormatOpenAIImage
		}
		if c.Request.URL.Path == "/v1/messages" {
			relayFormat = types.RelayFormatClaude
		}
		if strings.Contains(c.Request.URL.Path, "/v1beta/models") {
			relayFormat = types.RelayFormatGemini
		}
		if c.Request.URL.Path == "/v1/rerank" || c.Request.URL.Path == "/rerank" {
			relayFormat = types.RelayFormatRerank
		}
		if c.Request.URL.Path == "/v1/responses" {
			relayFormat = types.RelayFormatOpenAIResponses
		}
		if strings.HasPrefix(c.Request.URL.Path, "/v1/responses/compact") {
			relayFormat = types.RelayFormatOpenAIResponsesCompaction
		}
	}

	request, err := buildTestRequest(testModel, endpointType, channel, options)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeJsonMarshalFailed),
		}
	}

	info, err := relaycommon.GenRelayInfo(c, relayFormat, request, nil)

	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeGenRelayInfoFailed),
		}
	}

	info.IsChannelTest = true
	info.MonitorFirstResponseInChannelTest = options.monitorFirstResponse && options.isStream
	info.RequireValidFirstResponseEvent = info.MonitorFirstResponseInChannelTest
	defer func() {
		attemptResult := info.EndFirstResponseAttempt()
		if attemptResult.TimedOut {
			timeoutErr := types.NewUpstreamFirstResponseTimeoutError(attemptResult.TimeoutSeconds)
			result.context = c
			result.localErr = timeoutErr
			result.newAPIError = timeoutErr
			result.firstResponseTimedOut = true
			return
		}
		if protocolErr := info.FirstResponseProtocolError(); protocolErr != nil && result.localErr == nil && result.newAPIError == nil {
			result.context = c
			result.localErr = protocolErr
			result.newAPIError = types.NewOpenAIError(protocolErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
	}()
	info.InitChannelMeta(c)

	err = attachTestBillingRequestInput(info, request)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeJsonMarshalFailed),
		}
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeChannelModelMappedError),
		}
	}

	testModel = info.UpstreamModelName
	// 更新请求中的模型名称
	request.SetModelName(testModel)

	apiType, _ := common.ChannelType2APIType(channel.Type)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact &&
		!common.IsResponsesCompactAPIType(apiType) {
		return testResult{
			context:     c,
			localErr:    fmt.Errorf("responses compaction test is not supported for api type %d", apiType),
			newAPIError: types.NewError(fmt.Errorf("unsupported api type: %d", apiType), types.ErrorCodeInvalidApiType),
		}
	}
	adaptor := relay.GetAdaptor(apiType)
	if adaptor == nil {
		return testResult{
			context:     c,
			localErr:    fmt.Errorf("invalid api type: %d, adaptor is nil", apiType),
			newAPIError: types.NewError(fmt.Errorf("invalid api type: %d, adaptor is nil", apiType), types.ErrorCodeInvalidApiType),
		}
	}

	//// 创建一个用于日志的 info 副本，移除 ApiKey
	//logInfo := info
	//logInfo.ApiKey = ""
	common.SysLog(fmt.Sprintf("testing channel %d with model %s , info %+v ", channel.Id, testModel, info.ToString()))

	priceData, err := helper.ModelPriceHelper(c, info, 0, request.GetTokenCountMeta())
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest)),
		}
	}

	adaptor.Init(info)

	var convertedRequest any
	// 根据 RelayMode 选择正确的转换函数
	switch info.RelayMode {
	case relayconstant.RelayModeEmbeddings:
		// Embedding 请求 - request 已经是正确的类型
		if embeddingReq, ok := request.(*dto.EmbeddingRequest); ok {
			convertedRequest, err = adaptor.ConvertEmbeddingRequest(c, info, *embeddingReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid embedding request type"),
				newAPIError: types.NewError(errors.New("invalid embedding request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeImagesGenerations:
		// 图像生成请求 - request 已经是正确的类型
		if imageReq, ok := request.(*dto.ImageRequest); ok {
			convertedRequest, err = adaptor.ConvertImageRequest(c, info, *imageReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid image request type"),
				newAPIError: types.NewError(errors.New("invalid image request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeRerank:
		// Rerank 请求 - request 已经是正确的类型
		if rerankReq, ok := request.(*dto.RerankRequest); ok {
			convertedRequest, err = adaptor.ConvertRerankRequest(c, info.RelayMode, *rerankReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid rerank request type"),
				newAPIError: types.NewError(errors.New("invalid rerank request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeResponses:
		// Response 请求 - request 已经是正确的类型
		if responseReq, ok := request.(*dto.OpenAIResponsesRequest); ok {
			convertedRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *responseReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid response request type"),
				newAPIError: types.NewError(errors.New("invalid response request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeResponsesCompact:
		// Response compaction request - convert to OpenAIResponsesRequest before adapting
		switch req := request.(type) {
		case *dto.OpenAIResponsesCompactionRequest:
			convertedRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
				Model:              req.Model,
				Input:              req.Input,
				Instructions:       req.Instructions,
				PreviousResponseID: req.PreviousResponseID,
			})
		case *dto.OpenAIResponsesRequest:
			convertedRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *req)
		default:
			return testResult{
				context:     c,
				localErr:    errors.New("invalid response compaction request type"),
				newAPIError: types.NewError(errors.New("invalid response compaction request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	default:
		// Chat/Completion 等其他请求类型
		if generalReq, ok := request.(*dto.GeneralOpenAIRequest); ok {
			convertedRequest, err = adaptor.ConvertOpenAIRequest(c, info, generalReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid general request type"),
				newAPIError: types.NewError(errors.New("invalid general request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	}

	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeConvertRequestFailed),
		}
	}
	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeJsonMarshalFailed),
		}
	}

	//jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings)
	//if err != nil {
	//	return testResult{
	//		context:     c,
	//		localErr:    err,
	//		newAPIError: types.NewError(err, types.ErrorCodeConvertRequestFailed),
	//	}
	//}

	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			if fixedErr, ok := relaycommon.AsParamOverrideReturnError(err); ok {
				return testResult{
					context:     c,
					localErr:    fixedErr,
					newAPIError: relaycommon.NewAPIErrorFromParamOverride(fixedErr),
				}
			}
			return testResult{
				context:     c,
				localErr:    err,
				newAPIError: types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid),
			}
		}
	}

	requestBody := bytes.NewBuffer(jsonData)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(jsonData))
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError),
		}
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		if httpResp.StatusCode != http.StatusOK {
			err := service.RelayErrorHandler(c.Request.Context(), httpResp, true)
			common.SysError(common.RedactExactValue(fmt.Sprintf(
				"channel test bad response: channel_id=%d name=%s type=%d model=%s endpoint_type=%s status=%d err=%v",
				channel.Id,
				channel.Name,
				channel.Type,
				testModel,
				endpointType,
				httpResp.StatusCode,
				err,
			), options.nonce))
			return testResult{
				context:     c,
				localErr:    err,
				newAPIError: types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError),
			}
		}
	}
	usageA, respErr := adaptor.DoResponse(c, httpResp, info)
	if respErr != nil {
		return testResult{
			context:     c,
			localErr:    respErr,
			newAPIError: respErr,
		}
	}
	usage, usageErr := coerceTestUsage(usageA, options.isStream, info.GetEstimatePromptTokens())
	if usageErr != nil {
		return testResult{
			context:     c,
			localErr:    usageErr,
			newAPIError: types.NewOpenAIError(usageErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError),
		}
	}
	responseResult := w.Result()
	respBody, err := readTestResponseBody(responseResult.Body, options.isStream, options.nonce)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError),
		}
	}
	if bodyErr := validateTestResponseBody(info, respBody, options.isStream); bodyErr != nil {
		return testResult{
			context:     c,
			localErr:    bodyErr,
			newAPIError: types.NewOpenAIError(bodyErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError),
		}
	}
	info.SetEstimatePromptTokens(usage.PromptTokens)

	quota, tieredResult := settleTestQuota(info, priceData, usage)
	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	consumedTime := float64(milliseconds) / 1000.0
	other := buildTestLogOther(c, info, priceData, usage, tieredResult)
	model.RecordConsumeLog(c, testUserID, model.RecordConsumeLogParams{
		ChannelId:        channel.Id,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ModelName:        info.OriginModelName,
		TokenName:        "模型测试",
		Quota:            quota,
		Content:          "模型测试",
		UseTimeSeconds:   int(consumedTime),
		IsStream:         info.IsStream,
		Group:            info.UsingGroup,
		Other:            other,
	})
	common.SysLog(fmt.Sprintf("testing channel #%d, response: \n%s", channel.Id, common.RedactExactValue(string(respBody), options.nonce)))
	return testResult{
		context:     c,
		localErr:    nil,
		newAPIError: nil,
	}
}

func attachTestBillingRequestInput(info *relaycommon.RelayInfo, request dto.Request) error {
	if info == nil {
		return nil
	}

	input, err := helper.BuildBillingExprRequestInputFromRequest(request, info.RequestHeaders)
	if err != nil {
		return err
	}
	info.BillingRequestInput = &input
	return nil
}

func settleTestQuota(info *relaycommon.RelayInfo, priceData hosttypes.PriceData, usage *dto.Usage) (int, *billingexpr.TieredResult) {
	if usage != nil && info != nil && info.TieredBillingSnapshot != nil {
		isClaudeUsageSemantic := usage.UsageSemantic == "anthropic" || info.GetFinalRequestRelayFormat() == types.RelayFormatClaude
		usedVars := billingexpr.UsedVars(info.TieredBillingSnapshot.ExprString)
		if ok, quota, result := service.TryTieredSettle(info, service.BuildTieredTokenParams(usage, isClaudeUsageSemantic, usedVars)); ok {
			return quota, result
		}
	}

	quota := 0
	if !priceData.UsePrice {
		quota = usage.PromptTokens + int(math.Round(float64(usage.CompletionTokens)*priceData.CompletionRatio))
		quota = int(math.Round(float64(quota) * priceData.ModelRatio))
		if priceData.ModelRatio != 0 && quota <= 0 {
			quota = 1
		}
		return quota, nil
	}

	return int(priceData.ModelPrice * common.QuotaPerUnit), nil
}

func buildTestLogOther(c *gin.Context, info *relaycommon.RelayInfo, priceData hosttypes.PriceData, usage *dto.Usage, tieredResult *billingexpr.TieredResult) map[string]interface{} {
	other := service.GenerateTextOtherInfo(c, info, priceData.ModelRatio, priceData.GroupRatioInfo.GroupRatio, priceData.CompletionRatio,
		usage.PromptTokensDetails.CachedTokens, priceData.CacheRatio, priceData.ModelPrice, priceData.GroupRatioInfo.GroupSpecialRatio)
	if tieredResult != nil {
		service.InjectTieredBillingInfo(other, info, tieredResult)
	}
	return other
}

func coerceTestUsage(usageAny any, isStream bool, estimatePromptTokens int) (*dto.Usage, error) {
	switch u := usageAny.(type) {
	case *dto.Usage:
		return u, nil
	case dto.Usage:
		return &u, nil
	case nil:
		if !isStream {
			return nil, errors.New("usage is nil")
		}
		usage := &dto.Usage{
			PromptTokens: estimatePromptTokens,
		}
		usage.TotalTokens = usage.PromptTokens
		return usage, nil
	default:
		if !isStream {
			return nil, fmt.Errorf("invalid usage type: %T", usageAny)
		}
		usage := &dto.Usage{
			PromptTokens: estimatePromptTokens,
		}
		usage.TotalTokens = usage.PromptTokens
		return usage, nil
	}
}

const (
	maxStreamTestLogBytes        = 8 << 10
	maxStreamTestValidationBytes = 4 << 20
)

func readAndValidateStreamTestResponse(body io.Reader, sensitiveValue string) ([]byte, error) {
	preview := make([]byte, 0, maxStreamTestLogBytes)
	previewTruncated := false
	hasValidEvent := false
	var protocolErr error
	limitedBody := &io.LimitedReader{R: body, N: maxStreamTestValidationBytes + 1}
	scanner := helper.NewStreamScanner(limitedBody)
	eventType := ""
	dataLines := make([]string, 0, 1)

	validateEvent := func() {
		if strings.EqualFold(strings.TrimSpace(eventType), "error") && protocolErr == nil {
			protocolErr = errors.New("stream response contains an explicit error event")
		}
		if len(dataLines) == 0 {
			eventType = ""
			return
		}

		payload := []byte(strings.Join(dataLines, "\n"))
		eventType = ""
		dataLines = dataLines[:0]
		if relaycommon.IsIgnorableFirstResponseEvent(payload) {
			return
		}
		if !relaycommon.IsValidFirstResponseJSON(payload) {
			if protocolErr == nil {
				protocolErr = errors.New("stream response contains an invalid or error stream event")
			}
			return
		}
		hasValidEvent = true
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		redactedLine := []byte(common.RedactExactValue(string(line), sensitiveValue))
		if remaining := maxStreamTestLogBytes - len(preview); remaining > 0 {
			copyLength := len(redactedLine)
			if copyLength > remaining {
				copyLength = remaining
			}
			preview = append(preview, redactedLine[:copyLength]...)
			remaining -= copyLength
			if remaining > 0 {
				preview = append(preview, '\n')
				remaining--
			}
			if copyLength < len(redactedLine) || remaining == 0 {
				previewTruncated = true
			}
		} else {
			previewTruncated = true
		}

		if len(line) == 0 {
			validateEvent()
			continue
		}
		if line[0] == ':' {
			continue
		}

		field, value := string(line), ""
		if separator := bytes.IndexByte(line, ':'); separator >= 0 {
			field = string(line[:separator])
			value = string(line[separator+1:])
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
		}
		switch field {
		case "data":
			dataLines = append(dataLines, value)
		case "event":
			eventType = value
		case "id", "retry":
			// Valid SSE control fields do not contribute response content.
		}
	}
	validateEvent()

	marker := []byte("\n...[stream log preview truncated]")
	if previewTruncated {
		keep := maxStreamTestLogBytes - len(marker)
		if keep < 0 {
			keep = 0
		}
		if len(preview) > keep {
			preview = preview[:keep]
		}
		preview = append(preview, marker...)
	}
	if limitedBody.N == 0 {
		return preview, fmt.Errorf("stream response exceeds the %d-byte validation limit", maxStreamTestValidationBytes)
	}
	if err := scanner.Err(); err != nil {
		return preview, fmt.Errorf("stream response event exceeds the configured limit or could not be read: %w", err)
	}
	if protocolErr != nil {
		return preview, protocolErr
	}
	if !hasValidEvent {
		return preview, errors.New("stream response body does not contain a valid non-error stream event")
	}
	return preview, nil
}

func readTestResponseBody(body io.ReadCloser, isStream bool, sensitiveValue string) ([]byte, error) {
	defer func() { _ = body.Close() }()
	if isStream {
		return readAndValidateStreamTestResponse(body, sensitiveValue)
	}
	responseBody, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return []byte(common.RedactExactValue(string(responseBody), sensitiveValue)), nil
}

func detectErrorFromTestResponseBody(respBody []byte) error {
	b := bytes.TrimSpace(respBody)
	if len(b) == 0 {
		return nil
	}
	if message := detectErrorMessageFromJSONBytes(b); message != "" {
		return fmt.Errorf("upstream error: %s", message)
	}

	for _, line := range bytes.Split(b, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		if message := detectErrorMessageFromJSONBytes(payload); message != "" {
			return fmt.Errorf("upstream error: %s", message)
		}
	}

	return nil
}

func validateStreamTestResponseBody(respBody []byte) error {
	_, err := readAndValidateStreamTestResponse(bytes.NewReader(respBody), "")
	return err
}

func validateTestResponseBody(info *relaycommon.RelayInfo, respBody []byte, isStream bool) error {
	if isStream {
		if info != nil {
			if protocolErr := info.FirstResponseProtocolError(); protocolErr != nil {
				return protocolErr
			}
			if info.StreamStatus != nil {
				if !info.StreamStatus.IsNormalEnd() {
					return fmt.Errorf("stream response ended abnormally: %s", info.StreamStatus.Summary())
				}
				if info.StreamStatus.HasErrors() {
					return fmt.Errorf("stream response contains %d invalid or error events", info.StreamStatus.TotalErrorCount())
				}
			}
		}
		return nil
	}
	return detectErrorFromTestResponseBody(respBody)
}

func shouldUseStreamForAutomaticChannelTest(channel *model.Channel) bool {
	return channel != nil && channel.Type == constant.ChannelTypeCodex
}

func detectErrorMessageFromJSONBytes(jsonBytes []byte) string {
	if len(jsonBytes) == 0 {
		return ""
	}
	if jsonBytes[0] != '{' && jsonBytes[0] != '[' {
		return ""
	}
	errVal := gjson.GetBytes(jsonBytes, "error")
	if !errVal.Exists() || errVal.Type == gjson.Null {
		return ""
	}

	message := gjson.GetBytes(jsonBytes, "error.message").String()
	if message == "" {
		message = gjson.GetBytes(jsonBytes, "error.error.message").String()
	}
	if message == "" && errVal.Type == gjson.String {
		message = errVal.String()
	}
	if message == "" {
		message = errVal.Raw
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "upstream returned error payload"
	}
	return message
}

func buildChannelTestPrompt(options channelTestRequestOptions, defaultPrompt string) string {
	if strings.TrimSpace(options.message) != "" {
		return prependChannelTestNonce(options.message, options.nonce)
	}

	if options.sampleTokens <= 0 {
		return prependChannelTestNonce(defaultPrompt, options.nonce)
	}
	var builder strings.Builder
	builder.Grow(options.sampleTokens*7 + 24)
	for range options.sampleTokens {
		builder.WriteString("sample ")
	}
	builder.WriteString("Respond briefly.")
	return prependChannelTestNonce(builder.String(), options.nonce)
}

func newChannelTestNonce() string {
	return "nonce-" + common.GetRandomString(24)
}

func prependChannelTestNonce(prompt string, nonce string) string {
	if nonce == "" {
		return prompt
	}
	return nonce + "\n" + prompt
}

func buildChannelTestMessages(systemPrompt, userMessage string) []dto.Message {
	messages := make([]dto.Message, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, dto.Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}
	return append(messages, dto.Message{
		Role:    "user",
		Content: userMessage,
	})
}

func buildTestResponsesInput(messages []dto.Message) (json.RawMessage, error) {
	input, err := common.Marshal(messages)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(input), nil
}

func applyChannelTestParameters(request dto.Request, parameters *dto.ChannelTestParameters) {
	if parameters == nil {
		return
	}

	switch request := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if parameters.MaxTokens != nil {
			request.MaxTokens = parameters.MaxTokens
		} else if parameters.MaxOutputTokens != nil {
			request.MaxTokens = parameters.MaxOutputTokens
		}
		if parameters.MaxCompletionTokens != nil {
			request.MaxCompletionTokens = parameters.MaxCompletionTokens
		}
		if parameters.Temperature != nil {
			request.Temperature = parameters.Temperature
		}
		if parameters.TopP != nil {
			request.TopP = parameters.TopP
		}
		if parameters.TopK != nil {
			request.TopK = parameters.TopK
		}
		if parameters.FrequencyPenalty != nil {
			request.FrequencyPenalty = parameters.FrequencyPenalty
		}
		if parameters.PresencePenalty != nil {
			request.PresencePenalty = parameters.PresencePenalty
		}
		if parameters.ReasoningEffort != "" {
			request.ReasoningEffort = parameters.ReasoningEffort
		}
	case *dto.OpenAIResponsesRequest:
		maxOutputTokens := parameters.MaxOutputTokens
		if maxOutputTokens == nil {
			maxOutputTokens = parameters.MaxTokens
		}
		if maxOutputTokens == nil {
			maxOutputTokens = parameters.MaxCompletionTokens
		}
		if maxOutputTokens != nil {
			request.MaxOutputTokens = maxOutputTokens
		}
		if parameters.Temperature != nil {
			request.Temperature = parameters.Temperature
		}
		if parameters.TopP != nil {
			request.TopP = parameters.TopP
		}
		if parameters.ReasoningEffort != "" {
			request.Reasoning = &dto.Reasoning{Effort: parameters.ReasoningEffort}
		}
	}
}

func buildTestRequest(model string, endpointType string, channel *model.Channel, options channelTestRequestOptions) (dto.Request, error) {
	if options.prependNonce && options.nonce == "" {
		options.nonce = newChannelTestNonce()
	}
	if options.parameters != nil {
		if err := options.parameters.Validate(); err != nil {
			return nil, err
		}
	}

	if endpointType == "" || endpointType == dto.ChannelTestEndpointAuto {
		endpointType = normalizeChannelTestEndpoint(channel, model, dto.ChannelTestEndpointAuto)
	}

	var testPrompt string
	var testMessages []dto.Message
	if constant.EndpointType(endpointType) != constant.EndpointTypeImageGeneration &&
		constant.EndpointType(endpointType) != constant.EndpointTypeOpenAIResponseCompact {
		defaultPrompt := "hi"
		switch constant.EndpointType(endpointType) {
		case constant.EndpointTypeEmbeddings:
			defaultPrompt = "hello world"
		case constant.EndpointTypeJinaRerank:
			defaultPrompt = "What is Deep Learning?"
		}
		testPrompt = buildChannelTestPrompt(options, defaultPrompt)
		testMessages = buildChannelTestMessages(options.systemPrompt, testPrompt)
	}

	var request dto.Request
	if endpointType != "" {
		switch constant.EndpointType(endpointType) {
		case constant.EndpointTypeEmbeddings:
			request = &dto.EmbeddingRequest{Model: model, Input: []any{testPrompt}}
		case constant.EndpointTypeImageGeneration:
			imagePrompt := strings.TrimSpace(options.message)
			if imagePrompt == "" {
				imagePrompt = "a cute cat"
			}
			imagePrompt = prependChannelTestNonce(imagePrompt, options.nonce)
			request = &dto.ImageRequest{
				Model:  model,
				Prompt: imagePrompt,
				N:      lo.ToPtr(uint(1)),
				Size:   "1024x1024",
			}
		case constant.EndpointTypeJinaRerank:
			request = &dto.RerankRequest{
				Model:     model,
				Query:     testPrompt,
				Documents: []any{"Deep Learning is a subset of machine learning.", "Machine learning is a field of artificial intelligence."},
				TopN:      lo.ToPtr(2),
			}
		case constant.EndpointTypeOpenAIResponse:
			responseInput, err := buildTestResponsesInput(
				buildChannelTestMessages("", testPrompt),
			)
			if err != nil {
				return nil, err
			}
			var instructions json.RawMessage
			if strings.TrimSpace(options.systemPrompt) != "" {
				instructions, err = common.Marshal(options.systemPrompt)
				if err != nil {
					return nil, err
				}
			}
			request = &dto.OpenAIResponsesRequest{
				Model:           model,
				Input:           responseInput,
				Instructions:    instructions,
				MaxOutputTokens: lo.ToPtr(uint(16)),
				Stream:          lo.ToPtr(options.isStream),
			}
		case constant.EndpointTypeOpenAIResponseCompact:
			compactPrompt := strings.TrimSpace(options.message)
			if compactPrompt == "" {
				compactPrompt = "hi"
			}
			compactPrompt = prependChannelTestNonce(compactPrompt, options.nonce)
			testResponsesInput, err := buildTestResponsesInput(
				buildChannelTestMessages(options.systemPrompt, compactPrompt),
			)
			if err != nil {
				return nil, err
			}
			request = &dto.OpenAIResponsesCompactionRequest{
				Model: model,
				Input: testResponsesInput,
			}
		case constant.EndpointTypeAnthropic, constant.EndpointTypeGemini, constant.EndpointTypeOpenAI:
			var maxTokens *uint
			var maxCompletionTokens *uint
			if constant.EndpointType(endpointType) == constant.EndpointTypeGemini || strings.Contains(strings.ToLower(model), "gemini") {
				maxTokens = lo.ToPtr(uint(3000))
			} else if dto.IsOpenAIReasoningOModel(model) {
				maxCompletionTokens = lo.ToPtr(uint(16))
			} else if strings.Contains(strings.ToLower(model), "thinking") && !strings.Contains(strings.ToLower(model), "claude") {
				maxTokens = lo.ToPtr(uint(50))
			} else {
				maxTokens = lo.ToPtr(uint(16))
			}
			req := &dto.GeneralOpenAIRequest{
				Model:               model,
				Stream:              lo.ToPtr(options.isStream),
				Messages:            testMessages,
				MaxTokens:           maxTokens,
				MaxCompletionTokens: maxCompletionTokens,
			}
			if options.isStream {
				req.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
			}
			request = req
		}
	}

	if request == nil {
		return nil, fmt.Errorf("unsupported channel test endpoint type: %s", endpointType)
	}

	applyChannelTestParameters(request, options.parameters)
	return request, nil
}

func TestChannel(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.CacheGetChannel(channelId)
	if err != nil {
		channel, err = model.GetChannelById(channelId, true)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	//defer func() {
	//	if channel.ChannelInfo.IsMultiKey {
	//		go func() { _ = channel.SaveChannelInfo() }()
	//	}
	//}()
	testModel, options, err := parseChannelTestRequest(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tik := time.Now()
	requestCtx := context.Background()
	if c.Request != nil {
		requestCtx = c.Request.Context()
	}
	result := testChannel(requestCtx, channel, testUserID, testModel, options)
	if result.localErr != nil {
		resp := gin.H{
			"success": false,
			"message": result.localErr.Error(),
			"time":    0.0,
		}
		if result.newAPIError != nil {
			resp["error_code"] = result.newAPIError.GetErrorCode()
		}
		c.JSON(http.StatusOK, resp)
		return
	}
	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	go channel.UpdateResponseTime(milliseconds)
	consumedTime := float64(milliseconds) / 1000.0
	if result.newAPIError != nil {
		c.JSON(http.StatusOK, gin.H{
			"success":    false,
			"message":    result.newAPIError.Error(),
			"time":       consumedTime,
			"error_code": result.newAPIError.GetErrorCode(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"time":    consumedTime,
	})
}

// channelTestSummary records the outcome of one channel test cycle so the
// system task can persist a per-run result for history.
type channelTestSummary struct {
	Tested    int `json:"tested"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Disabled  int `json:"disabled"`
	Enabled   int `json:"enabled"`
}

// performChannelTests runs the channel test loop synchronously, honoring ctx
// cancellation so a system-task runner that loses its lease stops promptly. When
// report is non-nil it is called after each channel with (processed, total) so
// the system task can surface progress.
func performChannelTests(ctx context.Context, channels []*model.Channel, testUserID int, allowDisable bool, report func(processed, total int)) channelTestSummary {
	summary := channelTestSummary{}
	total := len(channels)
	for index, channel := range channels {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		if report != nil {
			report(index, total) // channels completed before this one
		}
		if channel.Status == common.ChannelStatusManuallyDisabled {
			continue
		}
		isChannelEnabled := channel.Status == common.ChannelStatusEnabled
		tik := time.Now()
		result := testChannel(ctx, channel, testUserID, "", channelTestRequestOptions{
			useChannelSettings:   true,
			monitorFirstResponse: true,
		})
		tok := time.Now()
		milliseconds := tok.Sub(tik).Milliseconds()
		if ctx != nil && ctx.Err() != nil {
			break
		}

		summary.Tested++

		// 总耗时阈值始终约束恢复；全局自动封禁开关只控制是否禁用已启用渠道。
		newAPIError, shouldBanChannel := evaluateAutomaticChannelTestResult(channel, result, milliseconds)

		if result.localErr != nil || newAPIError != nil {
			summary.Failed++
		} else {
			summary.Succeeded++
		}

		// disable channel
		if allowDisable && isChannelEnabled && shouldBanChannel && channel.GetAutoBan() {
			processChannelError(result.context, nil, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)
			summary.Disabled++
		}

		// enable channel
		if !isChannelEnabled && shouldRecoverChannelAfterTest(result, newAPIError, channel.Status) {
			if service.EnableChannel(channel.Id, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), channel.Name) {
				summary.Enabled++
			}
		}

		channel.UpdateResponseTime(milliseconds)
		if common.RequestInterval > 0 {
			if ctx == nil {
				time.Sleep(common.RequestInterval)
			} else {
				select {
				case <-ctx.Done():
					return summary
				case <-time.After(common.RequestInterval):
				}
			}
		}
	}
	if report != nil && (ctx == nil || ctx.Err() == nil) {
		report(total, total) // mark complete only when the full set was tested
	}
	return summary
}

// runChannelTestTask runs one synchronous channel test cycle for the system task
// runner (both the scheduled job and the manual "test all channels" trigger go
// through here). It honors ctx cancellation so a runner that loses its lease
// stops promptly. mode selects the channel set: an empty mode falls back to the
// configured monitor ChannelTestMode (scheduled behavior), while a manual
// trigger passes ChannelTestModeScheduledAll to test every channel. When notify
// is set the root user is notified on completion. Cross-instance execution is
// guarded by the system task per-type lock, so no process-local guard is needed.
func runChannelTestTask(ctx context.Context, mode string, notify bool, report func(processed, total int)) (channelTestSummary, error) {
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return channelTestSummary{}, err
	}
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return channelTestSummary{}, err
	}
	if strings.TrimSpace(mode) == "" {
		mode = operation_setting.GetMonitorSetting().ChannelTestMode
	}
	selected := selectChannelsForAutomaticTest(channels, mode)
	allowDisable := mode != operation_setting.ChannelTestModePassiveRecovery
	summary := performChannelTests(ctx, selected, testUserID, allowDisable, report)
	if notify && (ctx == nil || ctx.Err() == nil) {
		service.NotifyRootUser(dto.NotifyTypeChannelTest, "通道测试完成", "所有通道测试已完成")
	}
	return summary, nil
}

func selectChannelsForAutomaticTest(channels []*model.Channel, mode string) []*model.Channel {
	selected := make([]*model.Channel, 0, len(channels))
	for _, channel := range channels {
		if channel.Status == common.ChannelStatusManuallyDisabled {
			continue
		}
		if mode == operation_setting.ChannelTestModePassiveRecovery && channel.Status != common.ChannelStatusAutoDisabled {
			continue
		}
		selected = append(selected, channel)
	}
	return selected
}

// TestAllChannels enqueues a channel_test system task instead of running the
// test loop inline. If any channel_test task is already active, the manual run is
// rejected so the caller does not mistake a scheduled run for this manual one.
func TestAllChannels(c *gin.Context) {
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeChannelTest, channelTestTaskPayload{
		Mode:   operation_setting.ChannelTestModeScheduledAll,
		Notify: true,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !created {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "已有通道测试任务正在运行或等待中，不能启动本次手动任务",
			"data": gin.H{
				"task_id": task.TaskID,
				"status":  task.Status,
				"type":    task.Type,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"task_id": task.TaskID,
			"status":  task.Status,
		},
	})
}
