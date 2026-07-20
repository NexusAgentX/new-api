package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAutoGroupAffinityReturnsToRecoveredEarlierGroup(t *testing.T) {
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
	affinitySetting := operation_setting.GetChannelAffinitySetting()
	originalAffinitySetting := *affinitySetting
	originalRules := append([]operation_setting.ChannelAffinityRule(nil), affinitySetting.Rules...)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = true
	common.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["low-cost","fallback"]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"low-cost":"","fallback":""}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"low-cost":1,"fallback":1}`))

	affinitySetting.Enabled = true
	affinitySetting.SwitchOnSuccess = true
	affinitySetting.KeepOnChannelDisabled = false
	affinitySetting.DefaultTTLSeconds = 60
	affinitySetting.Rules = []operation_setting.ChannelAffinityRule{
		{
			Name:       "auto-group-recovery",
			ModelRegex: []string{"^gpt-test$"},
			PathRegex:  []string{"^/v1/responses$"},
			KeySources: []operation_setting.ChannelAffinityKeySource{
				{Type: "request_header", Key: "X-Affinity-Key"},
			},
			IncludeUsingGroup: true,
			IncludeRuleName:   true,
		},
	}
	service.ClearChannelAffinityCacheAll()

	t.Cleanup(func() {
		service.ClearChannelAffinityCacheAll()
		*affinitySetting = originalAffinitySetting
		affinitySetting.Rules = originalRules
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUserUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDBType, originalLogDBType)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RedisEnabled = originalRedisEnabled
	})

	priority := int64(0)
	autoBan := 1
	lowChannel := model.Channel{
		Id:       101,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "low-key",
		Status:   common.ChannelStatusManuallyDisabled,
		Name:     "low-cost-channel",
		Models:   "gpt-test",
		Group:    "low-cost",
		Priority: &priority,
		AutoBan:  &autoBan,
	}
	highChannel := model.Channel{
		Id:       202,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "fallback-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "fallback-channel",
		Models:   "gpt-test",
		Group:    "fallback",
		Priority: &priority,
		AutoBan:  &autoBan,
	}
	require.NoError(t, db.Create(&lowChannel).Error)
	require.NoError(t, db.Create(&highChannel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "low-cost", Model: "gpt-test", ChannelId: lowChannel.Id, Enabled: true, Priority: &priority}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "fallback", Model: "gpt-test", ChannelId: highChannel.Id, Enabled: true, Priority: &priority}).Error)
	model.InitChannelCache()

	engine := gin.New()
	engine.POST("/v1/responses",
		func(c *gin.Context) {
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "auto")
			common.SetContextKey(c, constant.ContextKeyUserGroup, "")
			c.Next()
		},
		Distribute(),
		func(c *gin.Context) {
			c.Header("X-Selected-Channel", strconv.Itoa(c.GetInt("channel_id")))
			c.Header("X-Selected-Group", common.GetContextKeyString(c, constant.ContextKeyAutoGroup))
			c.Status(http.StatusNoContent)
		},
	)

	performRequest := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Affinity-Key", "same-session")
		engine.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNoContent, recorder.Code)
		return recorder
	}

	first := performRequest()
	require.Equal(t, strconv.Itoa(highChannel.Id), first.Header().Get("X-Selected-Channel"))
	require.Equal(t, "fallback", first.Header().Get("X-Selected-Group"))

	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", lowChannel.Id).Update("status", common.ChannelStatusEnabled).Error)
	model.InitChannelCache()

	second := performRequest()
	require.Equal(t, strconv.Itoa(lowChannel.Id), second.Header().Get("X-Selected-Channel"))
	require.Equal(t, "low-cost", second.Header().Get("X-Selected-Group"))
}
