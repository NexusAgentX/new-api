package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDistributeUsesTokenMappedModelForLimitsAndChannelSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDBType := common.MainDatabaseType()
	originalLogDBType := common.LogDatabaseType()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRedisEnabled := common.RedisEnabled

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = true
	common.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDBType, originalLogDBType)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RedisEnabled = originalRedisEnabled
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	priority := int64(0)
	weight := uint(1)
	autoBan := 1
	channel := model.Channel{
		Id:       8101,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "mapped-model-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "mapped-model-channel",
		Models:   "glm-5.2",
		Group:    "default",
		Priority: &priority,
		Weight:   &weight,
		AutoBan:  &autoBan,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "glm-5.2",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
	model.InitChannelCache()

	engine := gin.New()
	engine.POST("/v1/chat/completions",
		func(c *gin.Context) {
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenModelMapping, map[string]string{"gpt-5.5": "glm-5.2"})
			common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
			common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"glm-5.2": true})
			c.Next()
		},
		TokenRequestCustomization(),
		func(c *gin.Context) {
			assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyRequestCustomizationDone))
			assert.Equal(t, "glm-5.2", common.GetContextKeyString(c, constant.ContextKeyRequestEffectiveModel))
			var request ModelRequest
			require.NoError(t, common.UnmarshalBodyReusable(c, &request))
			assert.Equal(t, "glm-5.2", request.Model)
			c.Next()
		},
		Distribute(),
		func(c *gin.Context) {
			var request ModelRequest
			require.NoError(t, common.UnmarshalBodyReusable(c, &request))
			c.JSON(http.StatusOK, gin.H{
				"model":         request.Model,
				"request_model": common.GetContextKeyString(c, constant.ContextKeyRequestModel),
				"channel_id":    c.GetInt("channel_id"),
			})
		},
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.5"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		Model        string `json:"model"`
		RequestModel string `json:"request_model"`
		ChannelId    int    `json:"channel_id"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "glm-5.2", response.Model)
	assert.Equal(t, "gpt-5.5", response.RequestModel)
	assert.Equal(t, channel.Id, response.ChannelId)
}
