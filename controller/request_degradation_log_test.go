package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProcessChannelErrorRecordsRequestDegradation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDBType := common.MainDatabaseType()
	originalLogDBType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	constant.ErrorLogEnabled = true
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))

	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDBType, originalLogDBType)
		common.RedisEnabled = originalRedisEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
		sqlDB, sqlErr := db.DB()
		require.NoError(t, sqlErr)
		require.NoError(t, sqlDB.Close())
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 91)
	ctx.Set("username", "request-owner")
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-test")
	ctx.Set("token_id", 7)
	ctx.Set("group", "default")
	ctx.Set("channel_id", 12)
	ctx.Set("channel_name", "first-upstream")
	ctx.Set("channel_type", 1)

	relayInfo := &relaycommon.RelayInfo{
		RequestDegradation: &hosttypes.RequestDegradation{
			Applied:               true,
			Reason:                hosttypes.RequestDegradationReasonNonReplayableReasoning,
			DroppedReasoningItems: 4,
		},
	}
	apiErr := relaytypes.NewErrorWithStatusCode(
		errors.New("upstream failed"),
		relaytypes.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)
	processChannelError(
		ctx,
		relayInfo,
		*relaytypes.NewChannelError(12, 1, "first-upstream", false, "", false),
		apiErr,
	)

	var recorded model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeError).First(&recorded).Error)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(recorded.Other, &other))
	degradation, ok := other["request_degradation"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, degradation["applied"])
	assert.Equal(t, hosttypes.RequestDegradationReasonNonReplayableReasoning, degradation["reason"])
	assert.Equal(t, float64(4), degradation["dropped_reasoning_items"])
}
