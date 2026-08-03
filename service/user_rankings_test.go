package service

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserRankingServiceTestDB(t *testing.T) {
	t.Helper()

	previousDB := model.DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousDataExportEnabled := common.DataExportEnabled

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.QuotaData{}))
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.DataExportEnabled = true
	resetUserRankingCache()

	t.Cleanup(func() {
		resetUserRankingCache()
		common.DataExportEnabled = previousDataExportEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		model.DB = previousDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func resetUserRankingCache() {
	userRankingCacheMu.Lock()
	defer userRankingCacheMu.Unlock()
	userRankingCache = map[string]userRankingCacheItem{}
	userRankingInFlight = map[string]*userRankingBuild{}
}

func seedUserRankingQuota(t *testing.T, row model.QuotaData) {
	t.Helper()
	if row.RequestType == "" {
		row.RequestType = model.UsageRequestTypeRegular
	}
	require.NoError(t, model.DB.Create(&row).Error)
}

func TestGetUserRankingsAggregatesAndIsolatesIdentity(t *testing.T) {
	setupUserRankingServiceTestDB(t)

	users := []model.User{
		{Id: 1, Username: "alice-login", DisplayName: "Alice", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "ranking-a"},
		{Id: 2, Username: "bob-login", DisplayName: "Alice", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "ranking-b"},
		{Id: 3, Username: "deleted-login", DisplayName: "Former user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "ranking-c"},
	}
	require.NoError(t, model.DB.Create(&users).Error)
	require.NoError(t, model.DB.Delete(&users[2]).Error)

	now := time.Now().Unix()
	rows := []model.QuotaData{
		{UserID: 1, Username: "alice-old", ModelName: "image-model", CreatedAt: now - 600, TokenUsed: 0, Quota: 300, Count: 1},
		{UserID: 1, Username: "alice-login", ModelName: "text-model", CreatedAt: now - 500, TokenUsed: 100, Quota: 100, Count: 2},
		{UserID: 1, Username: "alice-login", ModelName: "a-model", CreatedAt: now - 400, TokenUsed: 0, Quota: 25, Count: 1},
		{UserID: 1, Username: "alice-login", ModelName: "b-model", CreatedAt: now - 300, TokenUsed: 0, Quota: 25, Count: 1},
		{UserID: 1, Username: "alice-login", ModelName: "c-model", CreatedAt: now - 200, TokenUsed: 0, Quota: 25, Count: 1},
		{UserID: 1, Username: "alice-login", ModelName: "d-model", CreatedAt: now - 100, TokenUsed: 0, Quota: 25, Count: 1},
		{UserID: 2, Username: "bob-login", ModelName: "chat-model", CreatedAt: now - 100, TokenUsed: 100, Quota: 500, Count: 7},
		{UserID: 3, Username: "deleted-login", ModelName: "video-model", CreatedAt: now - 100, TokenUsed: 0, Quota: 600, Count: 1},
	}
	for _, row := range rows {
		seedUserRankingQuota(t, row)
	}

	regular, err := GetUserRankings("today", "quota", 20, false)
	require.NoError(t, err)
	require.True(t, regular.DataAvailable)
	require.Equal(t, UserRankingMetricSemantics{
		Count:  "consumption_records",
		Tokens: "prompt_completion_tokens",
		Quota:  "gross_quota",
	}, regular.Metrics)
	require.Len(t, regular.Users, 3)
	encodedRegular, err := common.Marshal(regular)
	require.NoError(t, err)
	assert.NotContains(t, string(encodedRegular), "alice-login")
	assert.NotContains(t, string(encodedRegular), "deleted-login")
	assert.NotContains(t, string(encodedRegular), "user_id")
	assert.NotContains(t, string(encodedRegular), "email")

	assert.True(t, regular.Users[0].Deleted)
	assert.Equal(t, "Deleted user", regular.Users[0].DisplayName)
	assert.Empty(t, regular.Users[0].Username)
	assert.Equal(t, int64(600), regular.Users[0].Quota)

	alice := regular.Users[1]
	assert.Equal(t, 2, alice.Rank)
	assert.Equal(t, "Alice", alice.DisplayName)
	assert.Equal(t, "a*********n", alice.Username)
	assert.Equal(t, int64(100), alice.Tokens)
	assert.Equal(t, int64(500), alice.Quota)
	assert.Equal(t, int64(7), alice.Count)
	require.Len(t, alice.TopModels, 5)
	assert.Equal(t, "image-model", alice.TopModels[0].ModelName)
	assert.Zero(t, alice.TopModels[0].Tokens)
	assert.Equal(t, 0.6, alice.TopModels[0].Share)
	assert.Equal(t, "text-model", alice.TopModels[1].ModelName)
	assert.Equal(t, 0.2, alice.TopModels[1].Share)
	assert.Equal(t, "a-model", alice.TopModels[2].ModelName)
	assert.Equal(t, "b-model", alice.TopModels[3].ModelName)
	assert.Equal(t, "c-model", alice.TopModels[4].ModelName)

	bob := regular.Users[2]
	assert.Equal(t, 3, bob.Rank, "equal quota is ordered by user_id")
	assert.Equal(t, "Alice", bob.DisplayName)
	assert.Equal(t, "b*******n", bob.Username)

	seedUserRankingQuota(t, model.QuotaData{
		UserID: 1, Username: "alice-renamed", ModelName: "new-model", CreatedAt: now, Quota: 900, Count: 1,
	})
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{
		"username":     "alice-renamed",
		"display_name": "Alice renamed",
	}).Error)

	admin, err := GetUserRankings("today", "quota", 20, true)
	require.NoError(t, err)
	require.Len(t, admin.Users, 3)
	assert.Equal(t, int64(500), admin.Users[1].Quota, "aggregate remains on the five-minute cache")
	assert.Equal(t, "Alice renamed", admin.Users[1].DisplayName, "identity is resolved after the aggregate cache")
	assert.Equal(t, "alice-renamed", admin.Users[1].Username)
}

func TestGetUserRankingsHandlesZeroMetricAndEmptyData(t *testing.T) {
	setupUserRankingServiceTestDB(t)

	empty, err := GetUserRankings("today", "tokens", 20, false)
	require.NoError(t, err)
	require.NotNil(t, empty.Users)
	assert.Empty(t, empty.Users)

	user := model.User{Id: 1, Username: "video-user", DisplayName: "Video user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	seedUserRankingQuota(t, model.QuotaData{
		UserID: 1, Username: user.Username, ModelName: "video-model", CreatedAt: time.Now().Unix(), Quota: 100, Count: 1,
	})
	resetUserRankingCache()

	response, err := GetUserRankings("today", "tokens", 20, false)
	require.NoError(t, err)
	require.Len(t, response.Users, 1)
	require.Len(t, response.Users[0].TopModels, 1)
	assert.Zero(t, response.Users[0].TopModels[0].Share)
	assert.False(t, math.IsNaN(response.Users[0].TopModels[0].Share))
	assert.False(t, math.IsInf(response.Users[0].TopModels[0].Share, 0))
}

func TestGetUserRankingsValidationAndUnavailableState(t *testing.T) {
	setupUserRankingServiceTestDB(t)

	tests := []struct {
		name   string
		period string
		sortBy string
		limit  int
		target error
	}{
		{name: "year is unsupported", period: "year", sortBy: "tokens", limit: 20, target: ErrInvalidUserRankingPeriod},
		{name: "unknown sort", period: "today", sortBy: "cost", limit: 20, target: ErrInvalidUserRankingSort},
		{name: "limit too small", period: "today", sortBy: "tokens", limit: 0, target: ErrInvalidUserRankingLimit},
		{name: "limit too large", period: "today", sortBy: "tokens", limit: 51, target: ErrInvalidUserRankingLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := GetUserRankings(test.period, test.sortBy, test.limit, false)
			require.ErrorIs(t, err, test.target)
		})
	}

	common.DataExportEnabled = false
	response, err := GetUserRankings("today", "tokens", 20, false)
	require.NoError(t, err)
	assert.False(t, response.DataAvailable)
	assert.Equal(t, "data_export_disabled", response.UnavailableReason)
	assert.Empty(t, response.Users)
}
