package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

const ChannelFailureSampleTTL = 24 * time.Hour

// ChannelFailureSample is the bounded, header-free request baseline captured by
// the relay attempt that transitions a channel to auto-disabled.
type ChannelFailureSample struct {
	ChannelId      int    `json:"channel_id" gorm:"primaryKey"`
	DisableEventId int64  `json:"disable_event_id" gorm:"index"`
	RequestBody    []byte `json:"-" gorm:"not null"`
	RequestPath    string `json:"request_path" gorm:"type:varchar(255);not null"`
	RelayFormat    string `json:"relay_format" gorm:"type:varchar(64);not null"`
	Model          string `json:"model" gorm:"type:varchar(191);not null"`
	Stream         bool   `json:"stream"`
	RequestId      string `json:"request_id" gorm:"type:varchar(64)"`
	CapturedAt     int64  `json:"captured_at" gorm:"index"`
	BodySize       int64  `json:"body_size"`
}

func (ChannelFailureSample) TableName() string {
	return "channel_failure_samples"
}

// SaveChannelFailureSampleIfCurrent rejects a delayed sample after any newer
// channel status transition, while keeping persistence separate from disabling.
func SaveChannelFailureSampleIfCurrent(sample *ChannelFailureSample) (bool, error) {
	if sample == nil || sample.ChannelId <= 0 || sample.DisableEventId <= 0 {
		return false, nil
	}

	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	stored := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).Select("id", "status").Where("id = ?", sample.ChannelId).First(&channel).Error; err != nil {
			return err
		}
		if channel.Status != common.ChannelStatusAutoDisabled {
			return nil
		}
		var latestEvent ChannelStatusEvent
		result := tx.Select("id").Where("channel_id = ?", sample.ChannelId).Order("id DESC").Limit(1).Find(&latestEvent)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 || latestEvent.Id != sample.DisableEventId {
			return nil
		}
		if err := saveChannelFailureSample(tx, sample); err != nil {
			return err
		}
		stored = true
		return nil
	})
	return stored, err
}

func saveChannelFailureSample(tx *gorm.DB, sample *ChannelFailureSample) error {
	if sample == nil {
		return nil
	}
	// Never let GORM interpolate a captured request body into SQL diagnostics.
	tx = tx.Session(&gorm.Session{Logger: tx.Logger.LogMode(gormlogger.Silent)})
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"disable_event_id",
			"request_body",
			"request_path",
			"relay_format",
			"model",
			"stream",
			"request_id",
			"captured_at",
			"body_size",
		}),
	}).Create(sample).Error
}

func GetChannelFailureSample(channelId int) (*ChannelFailureSample, error) {
	var sample ChannelFailureSample
	result := DB.Where("channel_id = ?", channelId).Limit(1).Find(&sample)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &sample, nil
}

func DeleteExpiredChannelFailureSamples(now time.Time) error {
	cutoff := now.Add(-ChannelFailureSampleTTL).Unix()
	return DB.Where("captured_at < ?", cutoff).Delete(&ChannelFailureSample{}).Error
}
