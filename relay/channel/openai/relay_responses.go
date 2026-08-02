package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if responsesCompatibilityFixEnabled(info) {
		normalizedBody, normalizedIDs, normalizeErr := helper.NormalizeResponsesResponseJSON(
			responseBody,
			helper.NewResponsesItemIDNormalizer(),
		)
		if normalizeErr != nil {
			return nil, types.NewOpenAIError(normalizeErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		responseBody = normalizedBody
		info.AddNormalizedResponsesResponseItemIDs(normalizedIDs)
		if normalizedIDs > 0 {
			logger.LogWarn(c, fmt.Sprintf("responses compatibility applied: normalized_response_item_ids=%d", normalizedIDs))
		}
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CacheWriteTokens = responsesResponse.Usage.InputTokensDetails.CacheWriteTokens
		}
	}
	// Count actual tool invocations from Output (not tool declarations).
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall:
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		case dto.BuildInCallFileSearchCall:
			info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
		case dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, output.Name)
		}
	}

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	if !relaycommon.IsNonBillableResponsesStatus(responsesResponse.Status) {
		for i := range responsesResponse.Output {
			idx := i
			imageCounter.Observe(&responsesResponse.Output[i], &idx)
		}
	}
	imageCounter.Commit(info)

	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	imageCommitted := false
	var firstEventError *types.NewAPIError
	forwardedEvent := false
	var itemIDNormalizer *helper.ResponsesItemIDNormalizer
	if responsesCompatibilityFixEnabled(info) {
		itemIDNormalizer = helper.NewResponsesItemIDNormalizer()
	}

	timeoutErr := helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if itemIDNormalizer != nil {
			normalizedData, normalizedIDs, err := helper.NormalizeResponsesStreamEventJSON(
				[]byte(data),
				itemIDNormalizer,
			)
			if err != nil {
				logger.LogError(c, "failed to normalize responses stream event: "+err.Error())
				sr.Error(err)
				return
			}
			data = string(normalizedData)
			info.AddNormalizedResponsesResponseItemIDs(normalizedIDs)
		}

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		if streamError := newResponsesStreamError(&streamResponse); streamError != nil {
			if !forwardedEvent {
				firstEventError = streamError
			} else {
				sendResponsesStreamData(c, streamResponse, data)
			}
			sr.Stop(streamError)
			return
		}
		sendResponsesStreamData(c, streamResponse, data)
		forwardedEvent = true
		switch streamResponse.Type {
		case "response.completed", "response.done":
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					if streamResponse.Response.Usage.InputTokens != 0 {
						usage.PromptTokens = streamResponse.Response.Usage.InputTokens
					}
					if streamResponse.Response.Usage.OutputTokens != 0 {
						usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
					}
					if streamResponse.Response.Usage.TotalTokens != 0 {
						usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
					}
					if streamResponse.Response.Usage.InputTokensDetails != nil {
						usage.PromptTokensDetails.CachedTokens = streamResponse.Response.Usage.InputTokensDetails.CachedTokens
						usage.PromptTokensDetails.CacheWriteTokens = streamResponse.Response.Usage.InputTokensDetails.CacheWriteTokens
					}
				}
				if !imageCommitted {
					if relaycommon.IsNonBillableResponsesStatus(streamResponse.Response.Status) {
						imageCounter.Reset()
						imageCounter.Commit(info)
						imageCommitted = true
					} else {
						for i := range streamResponse.Response.Output {
							idx := i
							imageCounter.Observe(&streamResponse.Response.Output[i], &idx)
						}
						imageCounter.Commit(info)
						imageCommitted = true
					}
				}
			} else if !imageCommitted {
				imageCounter.Commit(info)
				imageCommitted = true
			}
		case "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
			if !imageCommitted {
				imageCounter.Reset()
				imageCounter.Commit(info)
				imageCommitted = true
			}
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
				case dto.BuildInCallFileSearchCall:
					info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
				case dto.BuildInCallFunctionCall:
					info.CountBillableToolCall(dto.BuildInCallFunctionCall, streamResponse.Item.Name)
				case dto.ResponsesOutputTypeImageGenerationCall:
					if !imageCommitted {
						imageCounter.Observe(streamResponse.Item, streamResponse.OutputIndex)
					}
				}
			}
		}
	})
	if timeoutErr != nil {
		return nil, timeoutErr
	}
	if firstEventError != nil {
		info.ResetFirstResponseTimeForRetry()
		return nil, firstEventError
	}
	if itemIDNormalizer != nil && itemIDNormalizer.NormalizedCount() > 0 {
		logger.LogWarn(c, fmt.Sprintf(
			"responses compatibility applied: normalized_response_item_ids=%d",
			itemIDNormalizer.NormalizedCount(),
		))
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, nil
}

func responsesCompatibilityFixEnabled(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil && info.ChannelSetting.ResponsesCompatibilityFixEnabled()
}

func newResponsesStreamError(streamResponse *dto.ResponsesStreamResponse) *types.NewAPIError {
	openAIError := streamResponse.GetOpenAIError()
	if openAIError == nil {
		return nil
	}

	statusCode := http.StatusBadGateway
	errorType := strings.ToLower(strings.TrimSpace(openAIError.Type))
	errorCode := strings.ToLower(strings.TrimSpace(fmt.Sprint(openAIError.Code)))
	switch {
	case errorType == "rate_limit_error",
		errorCode == "rate_limit_error",
		errorCode == "rate_limit_exceeded",
		errorCode == "insufficient_quota":
		statusCode = http.StatusTooManyRequests
	case errorType == "invalid_request_error" || errorCode == "invalid_request_error":
		statusCode = http.StatusBadRequest
	case errorType == "authentication_error" || errorCode == "authentication_error":
		statusCode = http.StatusUnauthorized
	case errorType == "permission_error" || errorCode == "permission_error":
		statusCode = http.StatusForbidden
	case errorType == "not_found_error" || errorCode == "not_found_error":
		statusCode = http.StatusNotFound
	}
	return types.WithOpenAIError(*openAIError, statusCode)
}
