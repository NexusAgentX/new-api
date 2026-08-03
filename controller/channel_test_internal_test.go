package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relayopenai "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
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

func TestValidateChannelRequiresNewAPIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL *string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "blank", baseURL: common.GetPointer("  "), wantErr: true},
		{name: "configured", baseURL: common.GetPointer("https://new-api.example")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{
				Type:    constant.ChannelTypeNewAPI,
				BaseURL: test.baseURL,
			}

			err := validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "New API channel base URL cannot be empty")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNewAPIChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeNewAPI)

	require.True(t, ok)
	assert.Equal(t, constant.APITypeNewAPI, apiType)
	assert.Equal(t, "New API", constant.GetChannelTypeName(constant.ChannelTypeNewAPI))
	require.Greater(t, len(constant.ChannelBaseURLs), constant.ChannelTypeNewAPI)
	assert.Empty(t, constant.ChannelBaseURLs[constant.ChannelTypeNewAPI])
}

func TestResponsesCompactAPITypeSupport(t *testing.T) {
	tests := []struct {
		name    string
		apiType int
		want    bool
	}{
		{name: "OpenAI", apiType: constant.APITypeOpenAI, want: true},
		{name: "Codex", apiType: constant.APITypeCodex, want: true},
		{name: "Advanced Custom", apiType: constant.APITypeAdvancedCustom, want: true},
		{name: "Sub2API", apiType: constant.APITypeSub2API, want: true},
		{name: "New API", apiType: constant.APITypeNewAPI, want: true},
		{name: "Anthropic", apiType: constant.APITypeAnthropic, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, common.IsResponsesCompactAPIType(test.apiType))
		})
	}
}

func TestMultiprotocolGatewayEndpointTypes(t *testing.T) {
	want := []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeOpenAIAlphaSearch,
	}

	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeNewAPI, "gpt-5"))
	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeSub2API, "gpt-5"))
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

func TestCalculateChannelTestBillingUsesChannelRatioForTieredBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
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
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 0.08},
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	result := service.CalculateChannelTestBilling(ctx, info, &dto.Usage{
		PromptTokens: 1000,
		TotalTokens:  1000,
	})

	require.Equal(t, 120, result.Quota)
	assert.InDelta(t, 1500, result.QuotaBeforeGroup, 1e-12)
	assert.InDelta(t, 120, result.QuotaAfterGroupUnrounded, 1e-12)
	require.NotNil(t, result.TieredResult)
	require.Equal(t, "stream", result.TieredResult.MatchedTier)
}

func TestCalculateChannelTestBillingKeepsZeroUsageAtZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 0.1},
		},
	}

	result := service.CalculateChannelTestBilling(ctx, info, &dto.Usage{})

	assert.Zero(t, result.Quota)
	assert.Zero(t, result.QuotaBeforeGroup)
	assert.Zero(t, result.QuotaAfterGroupUnrounded)
	assert.True(t, result.HasQuotaCalculation)
}

func TestCoerceTestUsageDistinguishesReturnedUsageFromStreamFallback(t *testing.T) {
	fallback, returned, err := coerceTestUsage(nil, true, 42)
	require.NoError(t, err)
	assert.False(t, returned)
	assert.Equal(t, 42, fallback.PromptTokens)

	actual, returned, err := coerceTestUsage(dto.Usage{PromptTokens: 7}, true, 42)
	require.NoError(t, err)
	assert.True(t, returned)
	assert.Equal(t, 7, actual.PromptTokens)
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
	priceData := hosttypes.PriceData{
		GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 0.08},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, service.ChannelTestBillingResult{
		TieredResult: &billingexpr.TieredResult{MatchedTier: "base"},
	}, constant.ChannelCostModeUsageRatio)

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	assert.Equal(t, true, other["is_test"])
	assert.Equal(t, model.UsageRequestTypeChannelTest, other["request_type"])
	assert.Equal(t, constant.ChannelCostModeUsageRatio, other["channel_cost_mode"])
	assert.InDelta(t, 0.08, other["channel_cost_ratio"], 1e-12)
	require.NotEmpty(t, other["expr_b64"])
}

func TestResolveChannelTestBillingPolicyUsesConfiguredChannelCost(t *testing.T) {
	tests := []struct {
		name      string
		channel   model.Channel
		wantMode  string
		wantRatio float64
	}{
		{
			name:      "xtoken special",
			channel:   model.Channel{CostMode: constant.ChannelCostModeUsageRatio, UsageCostRatio: 0.08},
			wantMode:  constant.ChannelCostModeUsageRatio,
			wantRatio: 0.08,
		},
		{
			name:      "xtoken plus",
			channel:   model.Channel{CostMode: constant.ChannelCostModeUsageRatio, UsageCostRatio: 0.10},
			wantMode:  constant.ChannelCostModeUsageRatio,
			wantRatio: 0.10,
		},
		{
			name:     "fixed daily",
			channel:  model.Channel{CostMode: constant.ChannelCostModeFixedDaily, FixedDailyCostUSD: 2.1},
			wantMode: constant.ChannelCostModeFixedDaily,
		},
		{
			name:     "unconfigured",
			channel:  model.Channel{CostMode: constant.ChannelCostModeNone},
			wantMode: constant.ChannelCostModeNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, ratio, err := resolveChannelTestBillingPolicy(&test.channel)
			require.NoError(t, err)
			assert.Equal(t, test.wantMode, mode)
			assert.Equal(t, test.wantRatio, ratio)
		})
	}
}

func TestChannelTestRecordsConfiguredCostWithoutWalletConsumption(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.QuotaData{}, &model.Token{}))

	originalQuotaPerUnit := common.QuotaPerUnit
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalDataExportEnabled := common.DataExportEnabled
	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	originalCompletionRatios := ratio_setting.CompletionRatio2JSONString()
	originalCacheRatios := ratio_setting.CacheRatio2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalGroupGroupRatios := ratio_setting.GroupGroupRatio2JSONString()
	common.QuotaPerUnit = 500_000
	common.MemoryCacheEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"channel-cost-test-model":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"channel-cost-test-model":2}`))
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(`{"channel-cost-test-model":0.1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"LionOrg":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	service.InitHttpClient()
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.DataExportEnabled = originalDataExportEnabled
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(originalCompletionRatios))
		require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(originalCacheRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalGroupGroupRatios))
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-cost","object":"chat.completion","created":1,"model":"channel-cost-test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110}}`)
	}))
	defer upstream.Close()

	root := &model.User{
		Username: "channel-cost-root", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "LionOrg",
		Quota: 1_000_000, UsedQuota: 123, RequestCount: 7,
	}
	require.NoError(t, db.Create(root).Error)
	channel := &model.Channel{
		Name: "channel-cost-test", Type: constant.ChannelTypeOpenAI,
		Key: "test-key", BaseURL: common.GetPointer(upstream.URL),
		Models: "channel-cost-test-model", Group: "LionOrg",
		Status:   common.ChannelStatusEnabled,
		CostMode: constant.ChannelCostModeUsageRatio, UsageCostRatio: 0.08,
	}
	require.NoError(t, db.Create(channel).Error)

	result := testChannel(t.Context(), channel, root.Id, "channel-cost-test-model", channelTestRequestOptions{
		endpointType: string(constant.EndpointTypeOpenAI),
	})
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)

	var storedUser model.User
	require.NoError(t, db.First(&storedUser, root.Id).Error)
	assert.Equal(t, 1_000_000, storedUser.Quota)
	assert.Equal(t, 123, storedUser.UsedQuota)
	assert.Equal(t, 7, storedUser.RequestCount)

	var log model.Log
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&log).Error)
	assert.Equal(t, model.UsageRequestTypeChannelTest, log.RequestType)
	assert.Equal(t, 10, log.Quota)
	require.NotNil(t, log.QuotaBeforeGroup)
	require.NotNil(t, log.QuotaAfterGroupUnrounded)
	assert.InDelta(t, 120, *log.QuotaBeforeGroup, 1e-12)
	assert.InDelta(t, 9.6, *log.QuotaAfterGroupUnrounded, 1e-12)
	assert.Nil(t, log.ChannelRevenueUSD)
	require.NotNil(t, log.ChannelCostUSD)
	require.NotNil(t, log.ChannelCostRatio)
	assert.InDelta(t, 0.0000192, *log.ChannelCostUSD, 1e-12)
	assert.Equal(t, 0.08, *log.ChannelCostRatio)
	assert.Equal(t, constant.ChannelCostModeUsageRatio, log.ChannelCostMode)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, true, other["is_test"])
	assert.InDelta(t, 0.08, other["group_ratio"], 1e-12)
}

func TestRecordChannelTestConsumeLogChargesReturnedUsageOnFailure(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	originalQuotaPerUnit := common.QuotaPerUnit
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalDataExportEnabled := common.DataExportEnabled
	common.QuotaPerUnit = 500_000
	common.MemoryCacheEnabled = false
	common.DataExportEnabled = false
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.DataExportEnabled = originalDataExportEnabled
	})

	channel := &model.Channel{
		Name: "failed-channel-test-cost", CostMode: constant.ChannelCostModeUsageRatio,
		UsageCostRatio: 0.1,
	}
	require.NoError(t, db.Create(channel).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "admin")
	now := time.Now()
	info := &relaycommon.RelayInfo{
		UserId: 1, OriginModelName: "failed-test-model",
		UsingGroup: "LionOrg", StartTime: now, FirstResponseTime: now,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id},
		PriceData: hosttypes.PriceData{
			ModelRatio: 1, CompletionRatio: 2,
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 0.1, GroupSpecialRatio: -1},
		},
	}

	recordChannelTestConsumeLog(
		ctx,
		channel,
		1,
		info,
		info.PriceData,
		&dto.Usage{PromptTokens: 100, TotalTokens: 100},
		constant.ChannelCostModeUsageRatio,
		"failed",
		now,
	)

	var log model.Log
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&log).Error)
	assert.Equal(t, 10, log.Quota)
	assert.Nil(t, log.ChannelRevenueUSD)
	require.NotNil(t, log.ChannelCostUSD)
	assert.InDelta(t, 0.00002, *log.ChannelCostUSD, 1e-12)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "failed", other["channel_test_result"])
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

func setupFailureSampleReplayControllerTest(t *testing.T) (*model.User, *operation_setting.FirstResponseTimeoutSetting) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	originalAutomaticEnable := common.AutomaticEnableChannelEnabled
	originalAutomaticDisable := common.AutomaticDisableChannelEnabled
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalRequestInterval := common.RequestInterval
	originalStreamingTimeout := constant.StreamingTimeout
	firstResponseSetting := operation_setting.GetFirstResponseTimeoutSetting()
	originalFirstResponseSetting := *firstResponseSetting
	globalModelSetting := model_setting.GetGlobalSettings()
	originalPassThrough := globalModelSetting.PassThroughRequestEnabled
	originalModelRatios := ratio_setting.ModelRatio2JSONString()

	common.AutomaticEnableChannelEnabled = true
	common.AutomaticDisableChannelEnabled = false
	common.MemoryCacheEnabled = false
	common.LogConsumeEnabled = true
	common.RequestInterval = 0
	constant.StreamingTimeout = 30
	firstResponseSetting.DisableEnabled = false
	firstResponseSetting.FailureSampleReplayEnabled = true
	firstResponseSetting.FailureSampleMaxMB = 4
	globalModelSetting.PassThroughRequestEnabled = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":1}`))
	service.InitHttpClient()

	t.Cleanup(func() {
		common.AutomaticEnableChannelEnabled = originalAutomaticEnable
		common.AutomaticDisableChannelEnabled = originalAutomaticDisable
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.RequestInterval = originalRequestInterval
		constant.StreamingTimeout = originalStreamingTimeout
		*firstResponseSetting = originalFirstResponseSetting
		globalModelSetting.PassThroughRequestEnabled = originalPassThrough
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
	})

	root := &model.User{
		Username: "failure-sample-replay-root",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1_000_000,
	}
	require.NoError(t, db.Create(root).Error)
	return root, firstResponseSetting
}

func TestPerformChannelTestsRequiresFailureSampleReplayBeforeRecovery(t *testing.T) {
	root, _ := setupFailureSampleReplayControllerTest(t)

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

	var mutex sync.Mutex
	replayBodies := make([]string, 0, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		responseContent := "ok"
		if strings.Contains(string(body), "production-marker") {
			mutex.Lock()
			replayBodies = append(replayBodies, string(body))
			replayNumber := len(replayBodies)
			mutex.Unlock()
			if replayNumber == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, `{"error":{"message":"production-marker still failing"}}`)
				return
			}
			responseContent = "production-marker echoed"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"chatcmpl_replay","object":"chat.completion","created":1,"model":"mapped-model","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, responseContent)
	}))
	defer upstream.Close()

	stream := false
	disableThreshold := 0.0
	modelMapping := `{"gpt-4o-mini":"mapped-model"}`
	paramOverride := `{"temperature":0.25}`
	channel := &model.Channel{
		Name:          "failure-sample-recovery-test",
		Type:          constant.ChannelTypeOpenAI,
		Key:           "test-key",
		BaseURL:       common.GetPointer(upstream.URL),
		Models:        "gpt-4o-mini",
		ModelMapping:  &modelMapping,
		ParamOverride: &paramOverride,
		Group:         "default",
		Status:        common.ChannelStatusEnabled,
	}
	channel.SetSetting(dto.ChannelSettings{
		TestEndpointType:            string(constant.EndpointTypeOpenAI),
		TestStream:                  &stream,
		TestDisableThresholdSeconds: &disableThreshold,
		FailureSampleReplayEnabled:  true,
	})
	require.NoError(t, model.DB.Create(channel).Error)

	requestBody := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"production-marker"}],"temperature":0.9,"vendor_extension":{"keep":true}}`
	disableResult, err := model.UpdateChannelStatusWithEventDetailed(channel.Id, channel.Key, common.ChannelStatusAutoDisabled, "production failure", model.ChannelStatusChange{
		Source:       "first_response_policy",
		ReasonCode:   "first_response_timeout_rate",
		ReasonDetail: "production failure",
		RequestId:    "production-request",
	})
	require.NoError(t, err)
	require.True(t, disableResult.OverallChanged)
	require.NoError(t, model.DB.Create(&model.ChannelFailureSample{
		ChannelId:      channel.Id,
		DisableEventId: disableResult.StatusEventId,
		RequestBody:    []byte(requestBody),
		RequestPath:    "/v1/chat/completions",
		RelayFormat:    string(types.RelayFormatOpenAI),
		Model:          "gpt-4o-mini",
		RequestId:      "production-request",
		CapturedAt:     time.Now().Unix(),
		BodySize:       int64(len(requestBody)),
	}).Error)
	channel.Status = common.ChannelStatusAutoDisabled

	firstSummary := performChannelTests(t.Context(), []*model.Channel{channel}, root.Id, false, nil)
	assert.Equal(t, channelTestSummary{Tested: 1, Failed: 1}, firstSummary)
	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)

	secondSummary := performChannelTests(t.Context(), []*model.Channel{channel}, root.Id, false, nil)
	assert.Equal(t, channelTestSummary{Tested: 1, Succeeded: 1, Enabled: 1}, secondSummary)
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)

	mutex.Lock()
	capturedBodies := append([]string(nil), replayBodies...)
	mutex.Unlock()
	require.Len(t, capturedBodies, 2)
	firstNonce := strings.Fields(gjson.Get(capturedBodies[0], "messages.0.content").String())[0]
	secondNonce := strings.Fields(gjson.Get(capturedBodies[1], "messages.0.content").String())[0]
	assert.True(t, strings.HasPrefix(firstNonce, "nonce-"))
	assert.True(t, strings.HasPrefix(secondNonce, "nonce-"))
	assert.NotEqual(t, firstNonce, secondNonce)
	for _, body := range capturedBodies {
		assert.Equal(t, "mapped-model", gjson.Get(body, "model").String())
		assert.InDelta(t, 0.25, gjson.Get(body, "temperature").Float(), 0.0001)
		assert.Contains(t, gjson.Get(body, "messages.0.content").String(), "production-marker")
	}

	persistedSample, err := model.GetChannelFailureSample(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, requestBody, string(persistedSample.RequestBody))
	assert.Equal(t, disableResult.StatusEventId, persistedSample.DisableEventId)
	assert.NotContains(t, capturedLogs.String(), "production-marker")
}

func TestFailureSampleReplayPassThroughUsesIndependentNonceCopies(t *testing.T) {
	root, _ := setupFailureSampleReplayControllerTest(t)

	var mutex sync.Mutex
	capturedBodies := make([]string, 0, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mutex.Lock()
		capturedBodies = append(capturedBodies, string(body))
		mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_passthrough","object":"response","created_at":1,"status":"completed","model":"gpt-4o-mini","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()

	modelMapping := `{"gpt-4o-mini":"mapped-model"}`
	paramOverride := `{"temperature":0.25}`
	channel := &model.Channel{
		Name:          "failure-sample-passthrough-test",
		Type:          constant.ChannelTypeOpenAI,
		Key:           "test-key",
		BaseURL:       common.GetPointer(upstream.URL),
		Models:        "gpt-4o-mini",
		ModelMapping:  &modelMapping,
		ParamOverride: &paramOverride,
		Group:         "default",
		Status:        common.ChannelStatusAutoDisabled,
	}
	channel.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: true, FailureSampleReplayEnabled: true})
	require.NoError(t, model.DB.Create(channel).Error)

	requestBody := `{"model":"gpt-4o-mini","store":false,"input":[{"type":"reasoning","id":"bad-reasoning","summary":[]},{"type":"message","id":"bad-message","role":"user","content":[{"type":"input_text","text":"pass-through-marker"}]}],"temperature":0.9,"vendor_extension":{"keep":true}}`
	sample := &model.ChannelFailureSample{
		RequestBody: []byte(requestBody),
		RequestPath: "/v1/responses",
		RelayFormat: string(types.RelayFormatOpenAIResponses),
		Model:       "gpt-4o-mini",
		CapturedAt:  time.Now().Unix(),
		BodySize:    int64(len(requestBody)),
	}
	for range 2 {
		result := testChannel(t.Context(), channel, root.Id, sample.Model, channelTestRequestOptions{failureSample: sample})
		require.NoError(t, result.localErr)
		require.Nil(t, result.newAPIError)
	}

	mutex.Lock()
	bodies := append([]string(nil), capturedBodies...)
	mutex.Unlock()
	require.Len(t, bodies, 2)
	assert.Equal(t, requestBody, string(sample.RequestBody))
	nonces := make([]string, 0, 2)
	for _, body := range bodies {
		assert.True(t, gjson.Get(body, "vendor_extension.keep").Bool())
		assert.Equal(t, "gpt-4o-mini", gjson.Get(body, "model").String())
		assert.InDelta(t, 0.9, gjson.Get(body, "temperature").Float(), 0.0001)
		assert.Equal(t, int64(2), gjson.Get(body, "input.#").Int())
		assert.Equal(t, "bad-reasoning", gjson.Get(body, "input.0.id").String())
		assert.Equal(t, "bad-message", gjson.Get(body, "input.1.id").String())
		content := gjson.Get(body, "input.1.content.0.text").String()
		assert.Contains(t, content, "pass-through-marker")
		nonces = append(nonces, strings.Fields(content)[0])
	}
	assert.NotEqual(t, nonces[0], nonces[1])
}

func TestFailureSampleResponsesReplayHonorsDisabledCompatibilityFix(t *testing.T) {
	root, _ := setupFailureSampleReplayControllerTest(t)

	capturedBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody <- string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_fix_disabled","object":"response","created_at":1,"status":"completed","model":"mapped-model","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()

	modelMapping := `{"gpt-4o-mini":"mapped-model"}`
	channel := &model.Channel{
		Name:         "failure-sample-responses-fix-disabled-test",
		Type:         constant.ChannelTypeOpenAI,
		Key:          "test-key",
		BaseURL:      common.GetPointer(upstream.URL),
		Models:       "gpt-4o-mini",
		ModelMapping: &modelMapping,
		Group:        "default",
		Status:       common.ChannelStatusAutoDisabled,
	}
	channel.SetSetting(dto.ChannelSettings{
		FailureSampleReplayEnabled: true,
		ResponsesCompatibilityFix:  common.GetPointer(false),
	})
	require.NoError(t, model.DB.Create(channel).Error)

	requestBody := `{"model":"gpt-4o-mini","store":false,"input":[{"type":"reasoning","id":"bad-reasoning","summary":[]},{"type":"message","id":"bad-message","role":"user","content":[{"type":"input_text","text":"fix-disabled-marker"}]}]}`
	sample := &model.ChannelFailureSample{
		RequestBody: []byte(requestBody),
		RequestPath: "/v1/responses",
		RelayFormat: string(types.RelayFormatOpenAIResponses),
		Model:       "gpt-4o-mini",
		CapturedAt:  time.Now().Unix(),
		BodySize:    int64(len(requestBody)),
	}
	result := testChannel(t.Context(), channel, root.Id, sample.Model, channelTestRequestOptions{failureSample: sample})
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)

	body := <-capturedBody
	assert.Equal(t, requestBody, string(sample.RequestBody))
	assert.Equal(t, int64(2), gjson.Get(body, "input.#").Int())
	assert.Equal(t, "bad-reasoning", gjson.Get(body, "input.0.id").String())
	assert.Equal(t, "bad-message", gjson.Get(body, "input.1.id").String())
	assert.Equal(t, "mapped-model", gjson.Get(body, "model").String())
	assert.Contains(t, gjson.Get(body, "input.1.content.0.text").String(), "fix-disabled-marker")
}

func TestFailureSampleResponsesCompatibilityRunsBeforeParamOverride(t *testing.T) {
	root, _ := setupFailureSampleReplayControllerTest(t)

	capturedBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody <- string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_replay","status":"in_progress"}}`,
			`data: {"type":"response.completed","response":{"id":"resp_replay","status":"completed","model":"mapped-model","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			`data: [DONE]`,
			``,
		}, "\n"))
	}))
	defer upstream.Close()

	modelMapping := `{"gpt-4o-mini":"mapped-model"}`
	paramOverride := `{"operations":[{"path":"input.0.id","mode":"set","value":"override-after-compat"}]}`
	channel := &model.Channel{
		Name:          "failure-sample-responses-order-test",
		Type:          constant.ChannelTypeOpenAI,
		Key:           "test-key",
		BaseURL:       common.GetPointer(upstream.URL),
		Models:        "gpt-4o-mini",
		ModelMapping:  &modelMapping,
		ParamOverride: &paramOverride,
		Group:         "default",
		Status:        common.ChannelStatusAutoDisabled,
	}
	channel.SetSetting(dto.ChannelSettings{FailureSampleReplayEnabled: true})
	require.NoError(t, model.DB.Create(channel).Error)

	requestBody := `{"model":"gpt-4o-mini","store":false,"stream":true,"input":[{"type":"reasoning","id":"bad-reasoning","summary":[]},{"type":"message","id":"bad-message","role":"user","content":[{"type":"input_text","text":"responses-marker"}]}]}`
	sample := &model.ChannelFailureSample{
		RequestBody: []byte(requestBody),
		RequestPath: "/v1/responses",
		RelayFormat: string(types.RelayFormatOpenAIResponses),
		Model:       "gpt-4o-mini",
		Stream:      true,
		CapturedAt:  time.Now().Unix(),
		BodySize:    int64(len(requestBody)),
	}
	result := testChannel(t.Context(), channel, root.Id, sample.Model, channelTestRequestOptions{failureSample: sample})
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)

	body := <-capturedBody
	assert.Equal(t, requestBody, string(sample.RequestBody))
	assert.Equal(t, int64(1), gjson.Get(body, "input.#").Int())
	assert.Equal(t, "message", gjson.Get(body, "input.0.type").String())
	assert.Equal(t, "override-after-compat", gjson.Get(body, "input.0.id").String())
	assert.Equal(t, "mapped-model", gjson.Get(body, "model").String())
	assert.True(t, gjson.Get(body, "stream").Bool())
	assert.Contains(t, gjson.Get(body, "input.0.content.0.text").String(), "responses-marker")
}

func TestFailureSampleReplayWithoutSafeTextFallsBack(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Models: "gpt-4o-mini"}
	sample := &model.ChannelFailureSample{
		RequestBody: []byte(`{"model":"gpt-4o-mini","messages":[{"role":"tool","content":"tool result"}]}`),
		RequestPath: "/v1/chat/completions",
		RelayFormat: string(types.RelayFormatOpenAI),
		Model:       "gpt-4o-mini",
	}

	result := testChannel(t.Context(), channel, 0, sample.Model, channelTestRequestOptions{failureSample: sample})
	require.ErrorIs(t, result.localErr, errFailureSampleNotReplayable)
}

func TestChannelFailureSampleReplayFallsBackForUnavailableSamples(t *testing.T) {
	_, firstResponseSetting := setupFailureSampleReplayControllerTest(t)
	firstResponseSetting.FailureSampleMaxMB = 1

	channel := &model.Channel{
		Name:   "failure-sample-fallback-test",
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Models: "gpt-4o-mini",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	channel.SetSetting(dto.ChannelSettings{FailureSampleReplayEnabled: true})
	require.NoError(t, model.DB.Create(channel).Error)
	disableResult, err := model.UpdateChannelStatusWithEventDetailed(channel.Id, channel.Key, common.ChannelStatusAutoDisabled, "failure", model.ChannelStatusChange{
		Source: "relay_error", ReasonCode: "upstream_error", ReasonDetail: "failure",
	})
	require.NoError(t, err)
	require.True(t, disableResult.OverallChanged)
	channel.Status = common.ChannelStatusAutoDisabled

	_, replay := channelFailureSampleForReplay(channel, disableResult.StatusEventId)
	assert.False(t, replay, "a missing sample must use the existing recovery check")

	validBody := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`
	sample := &model.ChannelFailureSample{
		ChannelId:      channel.Id,
		DisableEventId: disableResult.StatusEventId,
		RequestBody:    []byte(validBody),
		RequestPath:    "/v1/chat/completions",
		RelayFormat:    string(types.RelayFormatOpenAI),
		Model:          "gpt-4o-mini",
		CapturedAt:     time.Now().Add(-model.ChannelFailureSampleTTL - time.Second).Unix(),
		BodySize:       int64(len(validBody)),
	}
	require.NoError(t, model.DB.Create(sample).Error)
	_, replay = channelFailureSampleForReplay(channel, disableResult.StatusEventId)
	assert.False(t, replay, "an expired sample must use the existing recovery check")

	oversizedBody := strings.Repeat("x", (1<<20)+1)
	sample.RequestBody = []byte(oversizedBody)
	sample.BodySize = int64(len(oversizedBody))
	sample.CapturedAt = time.Now().Unix()
	require.NoError(t, model.DB.Save(sample).Error)
	_, replay = channelFailureSampleForReplay(channel, disableResult.StatusEventId)
	assert.False(t, replay, "an oversized sample must use the existing recovery check")

	sample.RequestBody = []byte(validBody)
	sample.BodySize = int64(len(validBody))
	sample.RelayFormat = string(types.RelayFormatEmbedding)
	require.NoError(t, model.DB.Save(sample).Error)
	_, replay = channelFailureSampleForReplay(channel, disableResult.StatusEventId)
	assert.False(t, replay, "an unsupported format must use the existing recovery check")

	sample.RelayFormat = string(types.RelayFormatOpenAI)
	require.NoError(t, model.DB.Save(sample).Error)
	loaded, replay := channelFailureSampleForReplay(channel, disableResult.StatusEventId)
	require.True(t, replay)
	assert.Equal(t, validBody, string(loaded.RequestBody))

	firstResponseSetting.FailureSampleReplayEnabled = false
	_, replay = channelFailureSampleForReplay(channel, disableResult.StatusEventId)
	assert.False(t, replay, "a disabled global switch must use the existing recovery check")
	firstResponseSetting.FailureSampleReplayEnabled = true
	channel.SetSetting(dto.ChannelSettings{FailureSampleReplayEnabled: false})
	_, replay = channelFailureSampleForReplay(channel, disableResult.StatusEventId)
	assert.False(t, replay, "a disabled channel switch must use the existing recovery check")
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
	var statusEvent model.ChannelStatusEvent
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&statusEvent).Error)
	assert.Equal(t, "passive_recovery_test", statusEvent.Source)
	assert.Equal(t, "passive_recovery_succeeded", statusEvent.ReasonCode)

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
