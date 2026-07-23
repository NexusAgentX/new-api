package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	userRankingCacheTTL  = 5 * time.Minute
	userRankingTopModels = 5
	userRankingMaxLimit  = 50
)

var (
	ErrInvalidUserRankingPeriod = errors.New("invalid user ranking period")
	ErrInvalidUserRankingSort   = errors.New("invalid user ranking sort")
	ErrInvalidUserRankingLimit  = errors.New("invalid user ranking limit")
)

type UserRankingsResponse struct {
	Period            string                     `json:"period"`
	Sort              string                     `json:"sort"`
	GeneratedAt       string                     `json:"generated_at"`
	DataAvailable     bool                       `json:"data_available"`
	UnavailableReason string                     `json:"unavailable_reason,omitempty"`
	Metrics           UserRankingMetricSemantics `json:"metrics"`
	Users             []RankedUser               `json:"users"`
}

type UserRankingMetricSemantics struct {
	Count  string `json:"count"`
	Tokens string `json:"tokens"`
	Quota  string `json:"quota"`
}

type RankedUser struct {
	Rank        int               `json:"rank"`
	DisplayName string            `json:"display_name"`
	Username    string            `json:"username,omitempty"`
	Deleted     bool              `json:"deleted,omitempty"`
	Tokens      int64             `json:"tokens"`
	Quota       int64             `json:"quota"`
	Count       int64             `json:"count"`
	TopModels   []RankedUserModel `json:"top_models"`
}

type RankedUserModel struct {
	ModelName string  `json:"model_name"`
	Tokens    int64   `json:"tokens"`
	Quota     int64   `json:"quota"`
	Count     int64   `json:"count"`
	Share     float64 `json:"share"`
}

type userRankingAggregate struct {
	UserID    int
	Rank      int
	Tokens    int64
	Quota     int64
	Count     int64
	TopModels []RankedUserModel
}

type userRankingSnapshot struct {
	generatedAt time.Time
	users       []userRankingAggregate
}

type userRankingCacheItem struct {
	expiresAt time.Time
	data      *userRankingSnapshot
}

type userRankingBuild struct {
	done chan struct{}
	data *userRankingSnapshot
	err  error
}

var (
	userRankingCacheMu  sync.Mutex
	userRankingCache    = map[string]userRankingCacheItem{}
	userRankingInFlight = map[string]*userRankingBuild{}
)

func GetUserRankings(period string, sortBy string, limit int, isAdmin bool) (*UserRankingsResponse, error) {
	config, err := userRankingConfig(period)
	if err != nil {
		return nil, err
	}
	if sortBy != "tokens" && sortBy != "quota" && sortBy != "count" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidUserRankingSort, sortBy)
	}
	if limit < 1 || limit > userRankingMaxLimit {
		return nil, fmt.Errorf("%w: must be between 1 and %d", ErrInvalidUserRankingLimit, userRankingMaxLimit)
	}

	metrics := UserRankingMetricSemantics{
		Count:  "consumption_records",
		Tokens: "prompt_completion_tokens",
		Quota:  "gross_quota",
	}
	if !common.DataExportEnabled {
		return &UserRankingsResponse{
			Period:            config.id,
			Sort:              sortBy,
			GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
			DataAvailable:     false,
			UnavailableReason: "data_export_disabled",
			Metrics:           metrics,
			Users:             make([]RankedUser, 0),
		}, nil
	}

	cacheKey := fmt.Sprintf("%s:%s:%d", config.id, sortBy, limit)
	snapshot, err := getUserRankingSnapshot(cacheKey, config, sortBy, limit)
	if err != nil {
		return nil, err
	}

	userIDs := make([]int, 0, len(snapshot.users))
	for _, item := range snapshot.users {
		userIDs = append(userIDs, item.UserID)
	}
	identities, err := model.GetUserRankingIdentities(userIDs)
	if err != nil {
		return nil, err
	}
	identityByID := make(map[int]model.User, len(identities))
	for _, user := range identities {
		identityByID[user.Id] = user
	}

	users := make([]RankedUser, 0, len(snapshot.users))
	for _, item := range snapshot.users {
		row := RankedUser{
			Rank:      item.Rank,
			Tokens:    item.Tokens,
			Quota:     item.Quota,
			Count:     item.Count,
			TopModels: item.TopModels,
		}
		identity, exists := identityByID[item.UserID]
		if !exists || identity.DeletedAt.Valid {
			row.DisplayName = "Deleted user"
			row.Deleted = true
			users = append(users, row)
			continue
		}

		displayName := strings.TrimSpace(identity.DisplayName)
		if isAdmin {
			if displayName == "" {
				displayName = identity.Username
			}
			row.DisplayName = displayName
			row.Username = identity.Username
			users = append(users, row)
			continue
		}

		row.Username = maskRankingUsername(identity.Username)
		if displayName == "" || strings.EqualFold(displayName, identity.Username) {
			displayName = "User"
		}
		row.DisplayName = displayName
		users = append(users, row)
	}

	return &UserRankingsResponse{
		Period:        config.id,
		Sort:          sortBy,
		GeneratedAt:   snapshot.generatedAt.UTC().Format(time.RFC3339),
		DataAvailable: true,
		Metrics:       metrics,
		Users:         users,
	}, nil
}

func userRankingConfig(period string) (rankingPeriodConfig, error) {
	switch period {
	case "", "today":
		return rankingPeriodConfig{id: "today", duration: 24 * time.Hour}, nil
	case "week":
		return rankingPeriodConfig{id: "week", duration: 7 * 24 * time.Hour}, nil
	case "month":
		return rankingPeriodConfig{id: "month", duration: 30 * 24 * time.Hour}, nil
	default:
		return rankingPeriodConfig{}, fmt.Errorf("%w: %s", ErrInvalidUserRankingPeriod, period)
	}
}

func getUserRankingSnapshot(cacheKey string, config rankingPeriodConfig, sortBy string, limit int) (*userRankingSnapshot, error) {
	now := time.Now()
	userRankingCacheMu.Lock()
	if item, ok := userRankingCache[cacheKey]; ok && now.Before(item.expiresAt) {
		userRankingCacheMu.Unlock()
		return item.data, nil
	}
	if build, ok := userRankingInFlight[cacheKey]; ok {
		userRankingCacheMu.Unlock()
		<-build.done
		return build.data, build.err
	}

	build := &userRankingBuild{done: make(chan struct{})}
	userRankingInFlight[cacheKey] = build
	userRankingCacheMu.Unlock()

	build.data, build.err = buildUserRankingSnapshot(config, sortBy, limit, now)

	userRankingCacheMu.Lock()
	if build.err == nil {
		userRankingCache[cacheKey] = userRankingCacheItem{
			expiresAt: now.Add(userRankingCacheTTL),
			data:      build.data,
		}
	}
	delete(userRankingInFlight, cacheKey)
	close(build.done)
	userRankingCacheMu.Unlock()

	return build.data, build.err
}

func buildUserRankingSnapshot(config rankingPeriodConfig, sortBy string, limit int, now time.Time) (*userRankingSnapshot, error) {
	startTime, endTime := rankingTimeRange(config, now)
	totals, err := model.GetUserRankingTotals(startTime, endTime, sortBy, limit)
	if err != nil {
		return nil, err
	}

	userIDs := make([]int, 0, len(totals))
	for _, item := range totals {
		userIDs = append(userIDs, item.UserID)
	}
	modelTotals, err := model.GetUserRankingModelTotals(startTime, endTime, userIDs)
	if err != nil {
		return nil, err
	}
	modelsByUserID := make(map[int][]RankedUserModel, len(userIDs))
	for _, item := range modelTotals {
		modelsByUserID[item.UserID] = append(modelsByUserID[item.UserID], RankedUserModel{
			ModelName: item.ModelName,
			Tokens:    item.Tokens,
			Quota:     item.Quota,
			Count:     item.Count,
		})
	}

	users := make([]userRankingAggregate, 0, len(totals))
	for idx, item := range totals {
		models := modelsByUserID[item.UserID]
		sort.Slice(models, func(i, j int) bool {
			left := userRankingMetric(models[i].Tokens, models[i].Quota, models[i].Count, sortBy)
			right := userRankingMetric(models[j].Tokens, models[j].Quota, models[j].Count, sortBy)
			if left == right {
				return models[i].ModelName < models[j].ModelName
			}
			return left > right
		})
		if len(models) > userRankingTopModels {
			models = models[:userRankingTopModels]
		}
		userTotal := userRankingMetric(item.Tokens, item.Quota, item.Count, sortBy)
		for modelIdx := range models {
			modelMetric := userRankingMetric(models[modelIdx].Tokens, models[modelIdx].Quota, models[modelIdx].Count, sortBy)
			models[modelIdx].Share = rankingShare(modelMetric, userTotal)
		}

		users = append(users, userRankingAggregate{
			UserID:    item.UserID,
			Rank:      idx + 1,
			Tokens:    item.Tokens,
			Quota:     item.Quota,
			Count:     item.Count,
			TopModels: models,
		})
	}

	return &userRankingSnapshot{generatedAt: now, users: users}, nil
}

func userRankingMetric(tokens int64, quota int64, count int64, sortBy string) int64 {
	switch sortBy {
	case "quota":
		return quota
	case "count":
		return count
	default:
		return tokens
	}
}

func maskRankingUsername(username string) string {
	runes := []rune(strings.TrimSpace(username))
	switch len(runes) {
	case 0:
		return ""
	case 1:
		return "*"
	case 2:
		return string(runes[0]) + "*"
	default:
		return string(runes[0]) + strings.Repeat("*", len(runes)-2) + string(runes[len(runes)-1])
	}
}
