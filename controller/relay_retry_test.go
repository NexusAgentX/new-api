package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type retryBillingSettler struct {
	preConsumedQuota int
}

func (*retryBillingSettler) Settle(int) error           { return nil }
func (*retryBillingSettler) Refund(*gin.Context)        {}
func (*retryBillingSettler) NeedsRefund() bool          { return false }
func (b *retryBillingSettler) GetPreConsumedQuota() int { return b.preConsumedQuota }
func (b *retryBillingSettler) Reserve(targetQuota int) error {
	if targetQuota > b.preConsumedQuota {
		b.preConsumedQuota = targetQuota
	}
	return nil
}

func TestGetChannelRefreshesTieredBillingAfterCrossGroupRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDBType := common.MainDatabaseType()
	originalLogDBType := common.LogDatabaseType()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRedisEnabled := common.RedisEnabled
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalUserUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()

	initModelListColumnNames(t)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = false
	common.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["cheap","premium"]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"cheap":"","premium":""}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"cheap":0.1,"premium":0.2}`))

	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUserUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDBType, originalLogDBType)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RedisEnabled = originalRedisEnabled
		require.NoError(t, sqlDB.Close())
	})

	priority := int64(0)
	autoBan := 1
	channels := []model.Channel{
		{Id: 801, Type: constant.ChannelTypeOpenAI, Key: "cheap-key", Status: common.ChannelStatusEnabled, Name: "cheap", Models: "gpt-test", Group: "cheap", Priority: &priority, AutoBan: &autoBan},
		{Id: 802, Type: constant.ChannelTypeOpenAI, Key: "premium-key", Status: common.ChannelStatusEnabled, Name: "premium", Models: "gpt-test", Group: "premium", Priority: &priority, AutoBan: &autoBan},
	}
	for _, channel := range channels {
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&model.Ability{Group: channel.Group, Model: "gpt-test", ChannelId: channel.Id, Enabled: true, Priority: &priority}).Error)
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "")
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)

	const expr = `tier("base", p)`
	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta:           &relaycommon.ChannelMeta{},
		TokenGroup:            "auto",
		OriginModelName:       "gpt-test",
		FinalPreConsumedQuota: 50_000,
		Billing:               &retryBillingSettler{preConsumedQuota: 50_000},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                expr,
			ExprHash:                  billingexpr.ExprHashString(expr),
			GroupRatio:                0.10,
			EstimatedQuotaBeforeGroup: 500_000,
			EstimatedQuotaAfterGroup:  50_000,
			QuotaPerUnit:              500_000,
		},
	}
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   "gpt-test",
		RequestPath: "/v1/responses",
		Retry:       common.GetPointer(0),
	}

	first, firstErr := getChannel(ctx, relayInfo, retryParam)
	require.Nil(t, firstErr)
	require.Nil(t, service.PrepareTieredBillingForSelectedGroup(ctx, relayInfo))
	require.Equal(t, 801, first.Id)
	assert.Equal(t, "cheap", relayInfo.UsingGroup)
	assert.Equal(t, 0.10, relayInfo.TieredBillingSnapshot.GroupRatio)

	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", first.Id).Update("enabled", false).Error)
	retryParam.SetRetry(common.RetryTimes + 1)

	second, secondErr := getChannel(ctx, relayInfo, retryParam)
	require.Nil(t, secondErr)
	require.Nil(t, service.PrepareTieredBillingForSelectedGroup(ctx, relayInfo))
	require.Equal(t, 802, second.Id)
	assert.Equal(t, "premium", relayInfo.UsingGroup)
	assert.Equal(t, 0.20, relayInfo.PriceData.GroupRatioInfo.GroupRatio)
	assert.Equal(t, 0.20, relayInfo.TieredBillingSnapshot.GroupRatio)
	assert.Equal(t, 100_000, relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
	assert.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)

	ok, quota, result := service.TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 1_000_000})
	require.True(t, ok)
	require.NotNil(t, result)
	assert.Equal(t, 100_000, quota)
}

func TestRequestContextError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	requestCtx, cancel := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)

	require.Nil(t, requestContextError(ctx))
	cancel()

	err := requestContextError(ctx)
	require.NotNil(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 499, err.StatusCode)
	assert.Equal(t, types.ErrorCodeRequestCanceled, err.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(err))
	assert.False(t, types.IsRecordErrorLog(err))
}

func TestShouldRetryStopsWhenRequestContextDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testCases := []struct {
		name        string
		requestDone bool
		err         *types.NewAPIError
		want        bool
	}{
		{
			name: "active request retries generic 500",
			err:  types.NewErrorWithStatusCode(context.Canceled, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError),
			want: true,
		},
		{
			name:        "canceled request stops generic 500 retry",
			requestDone: true,
			err:         types.NewErrorWithStatusCode(context.Canceled, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError),
		},
		{
			name:        "canceled request stops channel error retry",
			requestDone: true,
			err:         types.NewErrorWithStatusCode(context.Canceled, types.ErrorCodeChannelInvalidKey, http.StatusInternalServerError),
		},
		{
			name:        "canceled request stops first response timeout retry",
			requestDone: true,
			err:         types.NewUpstreamFirstResponseTimeoutError(20),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			requestCtx, cancel := context.WithCancel(context.Background())
			if testCase.requestDone {
				cancel()
			} else {
				t.Cleanup(cancel)
			}
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)

			assert.Equal(t, testCase.want, shouldRetry(ctx, testCase.err, 1))
		})
	}
}

func TestShouldRetryFirstResponseTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setting := operation_setting.GetFirstResponseTimeoutSetting()
	original := *setting
	t.Cleanup(func() { *setting = original })

	testCases := []struct {
		name            string
		enabled         bool
		retriesLeft     int
		specificChannel bool
		want            bool
	}{
		{name: "enabled bypasses generic 524 skip", enabled: true, retriesLeft: 1, want: true},
		{name: "disabled", enabled: false, retriesLeft: 1, want: false},
		{name: "retry budget exhausted", enabled: true, retriesLeft: 0, want: false},
		{name: "specific channel remains fixed", enabled: true, retriesLeft: 1, specificChannel: true, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setting.RetryEnabled = testCase.enabled
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			if testCase.specificChannel {
				ctx.Set("specific_channel_id", 1)
			}

			err := types.NewUpstreamFirstResponseTimeoutError(20)
			require.Equal(t, 524, err.StatusCode)
			assert.Equal(t, testCase.want, shouldRetry(ctx, err, testCase.retriesLeft))
		})
	}
}
