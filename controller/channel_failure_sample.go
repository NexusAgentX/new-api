package controller

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

func buildChannelFailureSample(c *gin.Context, relayInfo *relaycommon.RelayInfo) *model.ChannelFailureSample {
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	if c == nil || relayInfo == nil || !setting.FailureSampleReplayEnabled || !relayInfo.ChannelSetting.FailureSampleReplayEnabled {
		return nil
	}
	if c.Request == nil || c.Request.URL == nil || !supportsFailureSampleRequest(relayInfo.RelayFormat, c.Request.URL.Path) {
		return nil
	}
	maxBytes := operation_setting.FailureSampleMaxBytes()
	if maxBytes <= 0 {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage.Size() <= 0 || storage.Size() > maxBytes {
		return nil
	}
	body, err := storage.Bytes()
	if err != nil || int64(len(body)) != storage.Size() {
		return nil
	}
	return &model.ChannelFailureSample{
		RequestBody: append([]byte(nil), body...),
		RequestPath: c.Request.URL.Path,
		RelayFormat: string(relayInfo.RelayFormat),
		Model:       relayInfo.OriginModelName,
		Stream:      relayInfo.IsStream,
		RequestId:   common.GetContextKeyString(c, common.RequestIdKey),
		CapturedAt:  time.Now().Unix(),
		BodySize:    int64(len(body)),
	}
}

func supportsFailureSampleReplay(relayFormat types.RelayFormat) bool {
	switch relayFormat {
	case types.RelayFormatOpenAI,
		types.RelayFormatOpenAIResponses,
		types.RelayFormatClaude,
		types.RelayFormatGemini:
		return true
	default:
		return false
	}
}

func supportsFailureSampleRequest(relayFormat types.RelayFormat, path string) bool {
	if !supportsFailureSampleReplay(relayFormat) || strings.Contains(path, "..") || strings.Contains(path, "//") {
		return false
	}
	switch relayFormat {
	case types.RelayFormatOpenAI:
		return path == "/v1/chat/completions" || path == "/v1/completions"
	case types.RelayFormatOpenAIResponses:
		return path == "/v1/responses"
	case types.RelayFormatClaude:
		return path == "/v1/messages"
	case types.RelayFormatGemini:
		return strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/")
	default:
		return false
	}
}
