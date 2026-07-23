package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type RankingQuotaTotal struct {
	ModelName   string `json:"model_name"`
	TotalTokens int64  `json:"total_tokens"`
}

type RankingQuotaBucket struct {
	ModelName string `json:"model_name"`
	Bucket    int64  `json:"bucket"`
	Tokens    int64  `json:"tokens"`
}

type UserRankingTotal struct {
	UserID int   `gorm:"column:user_id"`
	Tokens int64 `gorm:"column:tokens"`
	Quota  int64 `gorm:"column:quota"`
	Count  int64 `gorm:"column:count"`
}

type UserRankingModelTotal struct {
	UserID    int    `gorm:"column:user_id"`
	ModelName string `gorm:"column:model_name"`
	Tokens    int64  `gorm:"column:tokens"`
	Quota     int64  `gorm:"column:quota"`
	Count     int64  `gorm:"column:count"`
}

func GetRankingQuotaTotals(startTime int64, endTime int64) ([]RankingQuotaTotal, error) {
	var rows []RankingQuotaTotal
	query := DB.Table("quota_data").
		Select("model_name, sum(token_used) as total_tokens").
		Where("model_name <> ''").
		Group("model_name").
		Having("sum(token_used) > 0").
		Order("total_tokens DESC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func GetRankingQuotaBuckets(startTime int64, endTime int64, bucketSize int64) ([]RankingQuotaBucket, error) {
	if bucketSize <= 0 {
		bucketSize = 3600
	}
	bucketExpr := rankingBucketExpr(bucketSize)
	var rows []RankingQuotaBucket
	query := DB.Table("quota_data").
		Select(fmt.Sprintf("model_name, %s as bucket, sum(token_used) as tokens", bucketExpr)).
		Where("model_name <> ''").
		Group(fmt.Sprintf("model_name, %s", bucketExpr)).
		Having("sum(token_used) > 0").
		Order("bucket ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func GetUserRankingTotals(startTime int64, endTime int64, sortColumn string, limit int) ([]UserRankingTotal, error) {
	orderColumns := map[string]string{
		"tokens": "tokens DESC, user_id ASC",
		"quota":  "quota DESC, user_id ASC",
		"count":  "count DESC, user_id ASC",
	}
	order, ok := orderColumns[sortColumn]
	if !ok {
		return nil, fmt.Errorf("invalid user ranking sort: %s", sortColumn)
	}

	rows := make([]UserRankingTotal, 0)
	query := DB.Table("quota_data").
		Select("user_id, sum(token_used) as tokens, sum(quota) as quota, sum(count) as count").
		Where("user_id > 0").
		Group("user_id").
		Having("sum(count) > 0").
		Order(order).
		Limit(limit)
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	return rows, query.Find(&rows).Error
}

func GetUserRankingModelTotals(startTime int64, endTime int64, userIDs []int) ([]UserRankingModelTotal, error) {
	rows := make([]UserRankingModelTotal, 0)
	if len(userIDs) == 0 {
		return rows, nil
	}

	query := DB.Table("quota_data").
		Select("user_id, model_name, sum(token_used) as tokens, sum(quota) as quota, sum(count) as count").
		Where("user_id IN ? AND model_name <> ''", userIDs).
		Group("user_id, model_name").
		Having("sum(count) > 0").
		Order("user_id ASC, model_name ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	return rows, query.Find(&rows).Error
}

func GetUserRankingIdentities(userIDs []int) ([]User, error) {
	users := make([]User, 0)
	if len(userIDs) == 0 {
		return users, nil
	}

	err := DB.Unscoped().
		Select("id, username, display_name, deleted_at").
		Where("id IN ?", userIDs).
		Find(&users).Error
	return users, err
}

func rankingBucketExpr(bucketSize int64) string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return fmt.Sprintf("FLOOR(created_at / %d) * %d", bucketSize, bucketSize)
	}
	return fmt.Sprintf("(created_at / %d) * %d", bucketSize, bucketSize)
}

func applyRankingQuotaTimeRange(query *gorm.DB, startTime int64, endTime int64) *gorm.DB {
	if startTime > 0 {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("created_at <= ?", endTime)
	}
	return query
}
