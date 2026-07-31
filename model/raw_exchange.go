package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	RawExchangeCaptureOff        = "off"
	RawExchangeCaptureNonSuccess = "non_success"
	RawExchangeCaptureAll        = "all"

	RawExchangeOutcomeSuccess    = "success"
	RawExchangeOutcomeNonSuccess = "non_success"

	RawExchangeStatusPendingCommit   = "pending_commit"
	RawExchangeStatusStored          = "stored"
	RawExchangeStatusSkippedTooLarge = "skipped_too_large"
	RawExchangeStatusSkippedCapacity = "skipped_capacity"
	RawExchangeStatusCaptureFailed   = "capture_failed"
	RawExchangeStatusDeletePending   = "delete_pending"
	RawExchangeStatusDeleteFailed    = "delete_failed"
	RawExchangeStatusDeleted         = "deleted"
	RawExchangeStatusMissing         = "missing"
)

var (
	ErrRawExchangeCapacityExceeded   = errors.New("raw exchange storage capacity exceeded")
	ErrRawExchangeNotStored          = errors.New("raw exchange is not stored")
	ErrRawExchangeDownloadInProgress = errors.New("raw exchange download is in progress")
	ErrRawExchangeDeletionInProgress = errors.New("raw exchange deletion is in progress")
	ErrRawExchangeCommitStateChanged = errors.New("raw exchange commit state changed")
)

func NormalizeRawExchangeCaptureMode(mode string) (string, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return RawExchangeCaptureOff, nil
	}
	switch mode {
	case RawExchangeCaptureOff, RawExchangeCaptureNonSuccess, RawExchangeCaptureAll:
		return mode, nil
	default:
		return "", errors.New("raw_exchange_capture_mode must be off, non_success, or all")
	}
}

func EffectiveRawExchangeCaptureMode(mode string) string {
	normalized, err := NormalizeRawExchangeCaptureMode(mode)
	if err != nil {
		return RawExchangeCaptureOff
	}
	return normalized
}

type RawExchangeArchive struct {
	Id                      int64  `json:"id"`
	RequestId               string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId                  int    `json:"user_id" gorm:"index:idx_raw_exchange_user_created,priority:1;index:idx_raw_exchange_user_status,priority:1"`
	TokenId                 int    `json:"token_id" gorm:"index:idx_raw_exchange_token_created,priority:1"`
	CaptureMode             string `json:"capture_mode" gorm:"type:varchar(16)"`
	Outcome                 string `json:"outcome" gorm:"type:varchar(16);index:idx_raw_exchange_outcome_created,priority:1"`
	OutcomeReason           string `json:"outcome_reason" gorm:"type:varchar(64)"`
	Status                  string `json:"status" gorm:"type:varchar(32);index:idx_raw_exchange_status_expires,priority:1;index:idx_raw_exchange_user_status,priority:2;index:idx_raw_exchange_pending_retry,priority:1"`
	HttpStatus              int    `json:"http_status"`
	RequestMethod           string `json:"request_method" gorm:"type:varchar(16)"`
	RequestPath             string `json:"request_path" gorm:"type:varchar(512)"`
	RequestAvailable        bool   `json:"request_available"`
	ResponseAvailable       bool   `json:"response_available"`
	ResponseComplete        bool   `json:"response_complete"`
	RequestContentType      string `json:"request_content_type" gorm:"type:varchar(255)"`
	ResponseContentType     string `json:"response_content_type" gorm:"type:varchar(255)"`
	RequestContentEncoding  string `json:"request_original_content_encoding" gorm:"type:varchar(64)"`
	ResponseContentEncoding string `json:"response_content_encoding" gorm:"type:varchar(64)"`
	RequestBytes            int64  `json:"request_bytes"`
	ResponseBytes           int64  `json:"response_bytes"`
	RequestStoredBytes      int64  `json:"request_stored_bytes"`
	ResponseStoredBytes     int64  `json:"response_stored_bytes"`
	RequestSha256           string `json:"request_sha256,omitempty" gorm:"type:char(64)"`
	ResponseSha256          string `json:"response_sha256,omitempty" gorm:"type:char(64)"`
	StorageBackend          string `json:"storage_backend" gorm:"type:varchar(16)"`
	ObjectKey               string `json:"-" gorm:"type:varchar(512)"`
	NodeName                string `json:"node_name,omitempty" gorm:"type:varchar(128)"`
	ErrorCode               string `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	CommitAttempts          int    `json:"-"`
	NextCommitAt            int64  `json:"-" gorm:"index:idx_raw_exchange_pending_retry,priority:2"`
	CreatedAt               int64  `json:"created_at" gorm:"index:idx_raw_exchange_user_created,priority:2;index:idx_raw_exchange_token_created,priority:2;index:idx_raw_exchange_outcome_created,priority:2"`
	CommittedAt             int64  `json:"committed_at"`
	ExpiresAt               int64  `json:"expires_at" gorm:"index:idx_raw_exchange_status_expires,priority:2"`
	DeletedAt               int64  `json:"deleted_at"`
}

func (a *RawExchangeArchive) StoredBytes() int64 {
	if a == nil {
		return 0
	}
	return a.RequestStoredBytes + a.ResponseStoredBytes
}

type RawExchangeStorageUsage struct {
	Id        int   `json:"id" gorm:"primaryKey"`
	UsedBytes int64 `json:"used_bytes"`
	UpdatedAt int64 `json:"updated_at"`
}

type RawExchangeDownloadLease struct {
	Id        string `json:"id" gorm:"type:varchar(36);primaryKey"`
	ArchiveId int64  `json:"archive_id" gorm:"index:idx_raw_exchange_lease_archive_expires,priority:1"`
	UserId    int    `json:"user_id"`
	ExpiresAt int64  `json:"expires_at" gorm:"index:idx_raw_exchange_lease_archive_expires,priority:2;index"`
	CreatedAt int64  `json:"created_at"`
}

type RawExchangeArchiveSummary struct {
	RequestId           string `json:"request_id"`
	CaptureMode         string `json:"capture_mode"`
	Outcome             string `json:"outcome"`
	OutcomeReason       string `json:"outcome_reason,omitempty"`
	Status              string `json:"status"`
	ErrorCode           string `json:"error_code,omitempty"`
	HttpStatus          int    `json:"http_status"`
	RequestAvailable    bool   `json:"request_available"`
	ResponseAvailable   bool   `json:"response_available"`
	ResponseComplete    bool   `json:"response_complete"`
	RequestBytes        int64  `json:"request_bytes"`
	ResponseBytes       int64  `json:"response_bytes"`
	RequestStoredBytes  int64  `json:"request_stored_bytes"`
	ResponseStoredBytes int64  `json:"response_stored_bytes"`
	CreatedAt           int64  `json:"created_at"`
	ExpiresAt           int64  `json:"expires_at"`
	DeletedAt           int64  `json:"deleted_at,omitempty"`
}

type RawExchangeDownloadMetadata struct {
	*RawExchangeArchiveSummary
	RequestMethod           string `json:"request_method,omitempty"`
	RequestPath             string `json:"request_path,omitempty"`
	RequestContentType      string `json:"request_content_type,omitempty"`
	ResponseContentType     string `json:"response_content_type,omitempty"`
	RequestContentEncoding  string `json:"request_original_content_encoding,omitempty"`
	ResponseContentEncoding string `json:"response_content_encoding,omitempty"`
	RequestSha256           string `json:"request_sha256,omitempty"`
	ResponseSha256          string `json:"response_sha256,omitempty"`
}

func (a *RawExchangeArchive) Summary() *RawExchangeArchiveSummary {
	if a == nil {
		return nil
	}
	return &RawExchangeArchiveSummary{
		RequestId:           a.RequestId,
		CaptureMode:         a.CaptureMode,
		Outcome:             a.Outcome,
		OutcomeReason:       a.OutcomeReason,
		Status:              a.Status,
		ErrorCode:           a.ErrorCode,
		HttpStatus:          a.HttpStatus,
		RequestAvailable:    a.RequestAvailable,
		ResponseAvailable:   a.ResponseAvailable,
		ResponseComplete:    a.ResponseComplete,
		RequestBytes:        a.RequestBytes,
		ResponseBytes:       a.ResponseBytes,
		RequestStoredBytes:  a.RequestStoredBytes,
		ResponseStoredBytes: a.ResponseStoredBytes,
		CreatedAt:           a.CreatedAt,
		ExpiresAt:           a.ExpiresAt,
		DeletedAt:           a.DeletedAt,
	}
}

func (a *RawExchangeArchive) DownloadMetadata() *RawExchangeDownloadMetadata {
	if a == nil {
		return nil
	}
	return &RawExchangeDownloadMetadata{
		RawExchangeArchiveSummary: a.Summary(),
		RequestMethod:             a.RequestMethod,
		RequestPath:               a.RequestPath,
		RequestContentType:        a.RequestContentType,
		ResponseContentType:       a.ResponseContentType,
		RequestContentEncoding:    a.RequestContentEncoding,
		ResponseContentEncoding:   a.ResponseContentEncoding,
		RequestSha256:             a.RequestSha256,
		ResponseSha256:            a.ResponseSha256,
	}
}

func InitializeRawExchangeStorageUsage() error {
	usage := RawExchangeStorageUsage{Id: 1, UpdatedAt: common.GetTimestamp()}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&usage).Error
}

func BackfillRawExchangeCaptureModes() error {
	return DB.Model(&Token{}).
		Where("raw_exchange_capture_mode IS NULL OR raw_exchange_capture_mode = ?", "").
		Update("raw_exchange_capture_mode", RawExchangeCaptureOff).Error
}

func AttachRawExchangeSummaries(logs []*Log, userId *int) error {
	requestIds := make([]string, 0, len(logs))
	seen := make(map[string]struct{}, len(logs))
	for _, logEntry := range logs {
		if logEntry == nil || logEntry.RequestId == "" {
			continue
		}
		if _, exists := seen[logEntry.RequestId]; exists {
			continue
		}
		seen[logEntry.RequestId] = struct{}{}
		requestIds = append(requestIds, logEntry.RequestId)
	}
	if len(requestIds) == 0 {
		return nil
	}

	query := DB.Where("request_id IN ?", requestIds)
	if userId != nil {
		query = query.Where("user_id = ?", *userId)
	}
	var archives []RawExchangeArchive
	if err := query.Find(&archives).Error; err != nil {
		return err
	}
	byRequestId := make(map[string]*RawExchangeArchiveSummary, len(archives))
	for i := range archives {
		byRequestId[archives[i].RequestId] = archives[i].Summary()
	}
	for _, logEntry := range logs {
		if logEntry != nil {
			logEntry.RawExchange = byRequestId[logEntry.RequestId]
		}
	}
	return nil
}

func AcquireRawExchangeDownload(requestId string, userId int, leaseSeconds int64) (*RawExchangeArchive, string, error) {
	if leaseSeconds <= 0 {
		leaseSeconds = 300
	}
	var archive RawExchangeArchive
	leaseId := uuid.NewString()
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ?", common.GetTimestamp()).Delete(&RawExchangeDownloadLease{}).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("request_id = ? AND user_id = ?", requestId, userId).First(&archive).Error; err != nil {
			return err
		}
		if archive.Status != RawExchangeStatusStored || archive.ObjectKey == "" {
			return ErrRawExchangeNotStored
		}
		now := common.GetTimestamp()
		return tx.Create(&RawExchangeDownloadLease{
			Id:        leaseId,
			ArchiveId: archive.Id,
			UserId:    userId,
			ExpiresAt: now + leaseSeconds,
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, "", err
	}
	return &archive, leaseId, nil
}

func ReleaseRawExchangeDownload(leaseId string) error {
	if leaseId == "" {
		return nil
	}
	return DB.Where("id = ?", leaseId).Delete(&RawExchangeDownloadLease{}).Error
}

func BeginRawExchangeDelete(requestId string, userId int) (*RawExchangeArchive, bool, error) {
	var archive RawExchangeArchive
	alreadyDeleted := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		now := common.GetTimestamp()
		if err := tx.Where("expires_at <= ?", now).Delete(&RawExchangeDownloadLease{}).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("request_id = ? AND user_id = ?", requestId, userId).First(&archive).Error; err != nil {
			return err
		}
		switch archive.Status {
		case RawExchangeStatusDeleted:
			alreadyDeleted = true
			return nil
		case RawExchangeStatusDeletePending:
			return ErrRawExchangeDeletionInProgress
		}
		var activeDownloads int64
		if err := tx.Model(&RawExchangeDownloadLease{}).
			Where("archive_id = ? AND expires_at > ?", archive.Id, now).
			Count(&activeDownloads).Error; err != nil {
			return err
		}
		if activeDownloads > 0 {
			return ErrRawExchangeDownloadInProgress
		}
		return tx.Model(&RawExchangeArchive{}).Where("id = ?", archive.Id).Updates(map[string]any{
			"status":     RawExchangeStatusDeletePending,
			"error_code": "",
		}).Error
	})
	return &archive, alreadyDeleted, err
}

func CompleteRawExchangeDelete(archiveId int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var archive RawExchangeArchive
		if err := lockForUpdate(tx).Where("id = ?", archiveId).First(&archive).Error; err != nil {
			return err
		}
		if archive.Status == RawExchangeStatusDeleted {
			return nil
		}
		if archive.Status != RawExchangeStatusDeletePending {
			return ErrRawExchangeDeletionInProgress
		}
		storedBytes := archive.StoredBytes()
		capacityWasCommitted := archive.StorageBackend != "s3" || archive.CommittedAt > 0
		if capacityWasCommitted && storedBytes > 0 {
			var usage RawExchangeStorageUsage
			if err := lockForUpdate(tx).Where("id = ?", 1).First(&usage).Error; err != nil {
				return err
			}
			usage.UsedBytes -= storedBytes
			if usage.UsedBytes < 0 {
				usage.UsedBytes = 0
			}
			usage.UpdatedAt = common.GetTimestamp()
			if err := tx.Save(&usage).Error; err != nil {
				return err
			}
		}
		return tx.Model(&RawExchangeArchive{}).Where("id = ?", archive.Id).Updates(map[string]any{
			"status":                RawExchangeStatusDeleted,
			"object_key":            "",
			"request_available":     false,
			"response_available":    false,
			"request_stored_bytes":  int64(0),
			"response_stored_bytes": int64(0),
			"error_code":            "",
			"deleted_at":            common.GetTimestamp(),
		}).Error
	})
}

func MarkRawExchangeDeleteFailed(archiveId int64, code string) error {
	return DB.Model(&RawExchangeArchive{}).
		Where("id = ? AND status = ?", archiveId, RawExchangeStatusDeletePending).
		Updates(map[string]any{"status": RawExchangeStatusDeleteFailed, "error_code": code}).Error
}

func markRawExchangeArchivesDeletePending(tx *gorm.DB, tokenIds []int, userId int) error {
	if len(tokenIds) == 0 {
		return nil
	}
	return tx.Model(&RawExchangeArchive{}).
		Where("user_id = ? AND token_id IN ? AND status NOT IN ?", userId, tokenIds, []string{RawExchangeStatusDeleted, RawExchangeStatusDeletePending}).
		Updates(map[string]any{"status": RawExchangeStatusDeletePending, "error_code": "token_deleted"}).Error
}

func GetRawExchangeArchiveById(archiveId int64) (*RawExchangeArchive, error) {
	var archive RawExchangeArchive
	err := DB.Where("id = ?", archiveId).First(&archive).Error
	return &archive, err
}

func ListPendingRawExchangeDeletes(nodeName string, limit int) ([]RawExchangeArchive, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var archives []RawExchangeArchive
	err := DB.Where("status IN ? AND storage_backend = ? AND committed_at = 0 AND node_name = ?", []string{
		RawExchangeStatusDeletePending,
		RawExchangeStatusDeleteFailed,
	}, "s3", nodeName).
		Order("id ASC").
		Limit(limit).
		Find(&archives).Error
	return archives, err
}

func ListPendingRawExchangeCommits(nodeName string, limit int) ([]RawExchangeArchive, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var archives []RawExchangeArchive
	err := DB.Where("status = ? AND storage_backend = ? AND node_name = ? AND next_commit_at <= ?", RawExchangeStatusPendingCommit, "s3", nodeName, common.GetTimestamp()).
		Order("id ASC").
		Limit(limit).
		Find(&archives).Error
	return archives, err
}

func CompleteRawExchangeCommit(archiveId, committedAt, capacityBytes int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var archive RawExchangeArchive
		if err := lockForUpdate(tx).Where("id = ?", archiveId).First(&archive).Error; err != nil {
			return err
		}
		if archive.Status != RawExchangeStatusPendingCommit {
			return ErrRawExchangeCommitStateChanged
		}
		var usage RawExchangeStorageUsage
		if err := lockForUpdate(tx).Where("id = ?", 1).First(&usage).Error; err != nil {
			return err
		}
		storedBytes := archive.StoredBytes()
		if storedBytes < 0 || (capacityBytes > 0 && (storedBytes > capacityBytes || usage.UsedBytes > capacityBytes-storedBytes)) {
			return ErrRawExchangeCapacityExceeded
		}
		commitResult := tx.Model(&RawExchangeArchive{}).Where("id = ? AND status = ?", archive.Id, RawExchangeStatusPendingCommit).Updates(map[string]any{
			"status":         RawExchangeStatusStored,
			"committed_at":   committedAt,
			"error_code":     "",
			"next_commit_at": int64(0),
		})
		if commitResult.Error != nil {
			return commitResult.Error
		}
		if commitResult.RowsAffected != 1 {
			return ErrRawExchangeCommitStateChanged
		}
		return tx.Model(&RawExchangeStorageUsage{}).
			Where("id = ?", usage.Id).
			Updates(map[string]any{
				"used_bytes": gorm.Expr("used_bytes + ?", storedBytes),
				"updated_at": common.GetTimestamp(),
			}).Error
	})
}

func UpdateRawExchangePendingCommitError(archiveId int64, code string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var archive RawExchangeArchive
		if err := lockForUpdate(tx).Where("id = ?", archiveId).First(&archive).Error; err != nil {
			return err
		}
		if archive.Status != RawExchangeStatusPendingCommit {
			return ErrRawExchangeCommitStateChanged
		}
		attempts := archive.CommitAttempts + 1
		delaySeconds := int64(5)
		for attempt := 1; attempt < attempts && delaySeconds < 300; attempt++ {
			delaySeconds *= 2
		}
		if delaySeconds > 300 {
			delaySeconds = 300
		}
		return tx.Model(&RawExchangeArchive{}).Where("id = ? AND status = ?", archive.Id, RawExchangeStatusPendingCommit).Updates(map[string]any{
			"error_code":      code,
			"commit_attempts": attempts,
			"next_commit_at":  common.GetTimestamp() + delaySeconds,
		}).Error
	})
}

func FailRawExchangePendingCommit(archiveId int64, status, code string) error {
	if status != RawExchangeStatusCaptureFailed && status != RawExchangeStatusSkippedCapacity {
		return errors.New("invalid raw exchange pending commit failure status")
	}
	return DB.Model(&RawExchangeArchive{}).
		Where("id = ? AND status = ?", archiveId, RawExchangeStatusPendingCommit).
		Updates(map[string]any{
			"status":                status,
			"object_key":            "",
			"request_available":     false,
			"response_available":    false,
			"request_stored_bytes":  int64(0),
			"response_stored_bytes": int64(0),
			"error_code":            code,
			"next_commit_at":        int64(0),
		}).Error
}

func CreateRawExchangeArchive(archive *RawExchangeArchive, capacityBytes int64) error {
	if archive == nil {
		return errors.New("raw exchange archive is nil")
	}
	if archive.Status != RawExchangeStatusStored {
		return DB.Create(archive).Error
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var usage RawExchangeStorageUsage
		if err := lockForUpdate(tx).Where("id = ?", 1).First(&usage).Error; err != nil {
			return err
		}
		storedBytes := archive.StoredBytes()
		if storedBytes < 0 || (capacityBytes > 0 && (storedBytes > capacityBytes || usage.UsedBytes > capacityBytes-storedBytes)) {
			return ErrRawExchangeCapacityExceeded
		}
		if err := tx.Create(archive).Error; err != nil {
			return err
		}
		return tx.Model(&RawExchangeStorageUsage{}).
			Where("id = ?", usage.Id).
			Updates(map[string]any{
				"used_bytes": gorm.Expr("used_bytes + ?", storedBytes),
				"updated_at": common.GetTimestamp(),
			}).Error
	})
}

type RawExchangeCleanupFilter struct {
	AfterTimestamp  int64    `json:"after_timestamp,omitempty"`
	BeforeTimestamp int64    `json:"before_timestamp,omitempty"`
	UserId          int      `json:"user_id,omitempty"`
	TokenId         int      `json:"token_id,omitempty"`
	CaptureMode     string   `json:"capture_mode,omitempty"`
	Outcome         string   `json:"outcome,omitempty"`
	Status          string   `json:"status,omitempty"`
	Statuses        []string `json:"statuses,omitempty"`
}

type RawExchangeCleanupPreview struct {
	Count               int64 `json:"count"`
	RequestStoredBytes  int64 `json:"request_stored_bytes"`
	ResponseStoredBytes int64 `json:"response_stored_bytes"`
	TotalStoredBytes    int64 `json:"total_stored_bytes"`
}

type RawExchangeStatusStat struct {
	Status      string `json:"status"`
	Count       int64  `json:"count"`
	StoredBytes int64  `json:"stored_bytes"`
}

type RawExchangeUsageRank struct {
	ScopeId     int   `json:"scope_id"`
	Count       int64 `json:"count"`
	StoredBytes int64 `json:"stored_bytes"`
}

type RawExchangeAdminStats struct {
	TotalCount          int64                   `json:"total_count"`
	RequestBytes        int64                   `json:"request_bytes"`
	ResponseBytes       int64                   `json:"response_bytes"`
	RequestStoredBytes  int64                   `json:"request_stored_bytes"`
	ResponseStoredBytes int64                   `json:"response_stored_bytes"`
	UsedBytes           int64                   `json:"used_bytes"`
	OldestCreatedAt     int64                   `json:"oldest_created_at"`
	NewestCreatedAt     int64                   `json:"newest_created_at"`
	Statuses            []RawExchangeStatusStat `json:"statuses" gorm:"-"`
	TopUsers            []RawExchangeUsageRank  `json:"top_users" gorm:"-"`
	TopTokens           []RawExchangeUsageRank  `json:"top_tokens" gorm:"-"`
}

func PreviewRawExchangeCleanup(filter RawExchangeCleanupFilter) (RawExchangeCleanupPreview, error) {
	var preview RawExchangeCleanupPreview
	err := applyRawExchangeCleanupFilter(DB.Model(&RawExchangeArchive{}), filter).
		Select("COUNT(*) AS count, COALESCE(SUM(request_stored_bytes), 0) AS request_stored_bytes, COALESCE(SUM(response_stored_bytes), 0) AS response_stored_bytes").
		Scan(&preview).Error
	preview.TotalStoredBytes = preview.RequestStoredBytes + preview.ResponseStoredBytes
	return preview, err
}

func ListRawExchangeCleanupCandidates(filter RawExchangeCleanupFilter, afterId int64, limit int) ([]RawExchangeArchive, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var archives []RawExchangeArchive
	err := applyRawExchangeCleanupFilter(DB.Model(&RawExchangeArchive{}), filter).
		Where("id > ?", afterId).
		Order("id ASC").
		Limit(limit).
		Find(&archives).Error
	return archives, err
}

func GetRawExchangeAdminStats(limit int) (RawExchangeAdminStats, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	var stats RawExchangeAdminStats
	if err := DB.Model(&RawExchangeArchive{}).
		Select("COUNT(*) AS total_count, COALESCE(SUM(request_bytes), 0) AS request_bytes, COALESCE(SUM(response_bytes), 0) AS response_bytes, COALESCE(SUM(request_stored_bytes), 0) AS request_stored_bytes, COALESCE(SUM(response_stored_bytes), 0) AS response_stored_bytes, COALESCE(MIN(created_at), 0) AS oldest_created_at, COALESCE(MAX(created_at), 0) AS newest_created_at").
		Scan(&stats).Error; err != nil {
		return stats, err
	}
	var usage RawExchangeStorageUsage
	if err := DB.Where("id = ?", 1).First(&usage).Error; err != nil {
		return stats, err
	}
	stats.UsedBytes = usage.UsedBytes
	if err := DB.Model(&RawExchangeArchive{}).
		Select("status, COUNT(*) AS count, COALESCE(SUM(request_stored_bytes + response_stored_bytes), 0) AS stored_bytes").
		Group("status").
		Order("stored_bytes DESC").
		Scan(&stats.Statuses).Error; err != nil {
		return stats, err
	}
	if err := DB.Model(&RawExchangeArchive{}).
		Select("user_id AS scope_id, COUNT(*) AS count, COALESCE(SUM(request_stored_bytes + response_stored_bytes), 0) AS stored_bytes").
		Where("status = ?", RawExchangeStatusStored).
		Group("user_id").
		Order("stored_bytes DESC").
		Limit(limit).
		Scan(&stats.TopUsers).Error; err != nil {
		return stats, err
	}
	if err := DB.Model(&RawExchangeArchive{}).
		Select("token_id AS scope_id, COUNT(*) AS count, COALESCE(SUM(request_stored_bytes + response_stored_bytes), 0) AS stored_bytes").
		Where("status = ?", RawExchangeStatusStored).
		Group("token_id").
		Order("stored_bytes DESC").
		Limit(limit).
		Scan(&stats.TopTokens).Error; err != nil {
		return stats, err
	}
	if stats.Statuses == nil {
		stats.Statuses = make([]RawExchangeStatusStat, 0)
	}
	if stats.TopUsers == nil {
		stats.TopUsers = make([]RawExchangeUsageRank, 0)
	}
	if stats.TopTokens == nil {
		stats.TopTokens = make([]RawExchangeUsageRank, 0)
	}
	return stats, nil
}

func PrepareRawExchangeDeleteById(archiveId int64) (*RawExchangeArchive, bool, error) {
	var archive RawExchangeArchive
	alreadyDeleted := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		now := common.GetTimestamp()
		if err := tx.Where("expires_at <= ?", now).Delete(&RawExchangeDownloadLease{}).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("id = ?", archiveId).First(&archive).Error; err != nil {
			return err
		}
		if archive.Status == RawExchangeStatusDeleted {
			alreadyDeleted = true
			return nil
		}
		var activeDownloads int64
		if err := tx.Model(&RawExchangeDownloadLease{}).
			Where("archive_id = ? AND expires_at > ?", archive.Id, now).
			Count(&activeDownloads).Error; err != nil {
			return err
		}
		if activeDownloads > 0 {
			return ErrRawExchangeDownloadInProgress
		}
		if archive.Status == RawExchangeStatusDeletePending {
			if archive.ErrorCode == "token_deleted" {
				return nil
			}
			return ErrRawExchangeDeletionInProgress
		}
		return tx.Model(&RawExchangeArchive{}).Where("id = ?", archive.Id).Updates(map[string]any{
			"status":     RawExchangeStatusDeletePending,
			"error_code": "",
		}).Error
	})
	return &archive, alreadyDeleted, err
}

func ListStoredRawExchangeArchives(storageBackend, nodeName string, localShared bool, afterId int64, limit int) ([]RawExchangeArchive, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := DB.Where("id > ? AND status = ? AND storage_backend = ?", afterId, RawExchangeStatusStored, storageBackend)
	if storageBackend == "local" && !localShared {
		query = query.Where("node_name = ?", nodeName)
	}
	var archives []RawExchangeArchive
	err := query.Order("id ASC").Limit(limit).Find(&archives).Error
	return archives, err
}

func ExistingPendingRawExchangeObjectKeys(objectKeys []string) (map[string]struct{}, error) {
	existing := make(map[string]struct{}, len(objectKeys))
	if len(objectKeys) == 0 {
		return existing, nil
	}
	for start := 0; start < len(objectKeys); start += 500 {
		end := start + 500
		if end > len(objectKeys) {
			end = len(objectKeys)
		}
		var pendingKeys []string
		if err := DB.Model(&RawExchangeArchive{}).
			Where("status = ?", RawExchangeStatusPendingCommit).
			Where("object_key IN ?", objectKeys[start:end]).
			Pluck("object_key", &pendingKeys).Error; err != nil {
			return nil, err
		}
		for _, objectKey := range pendingKeys {
			existing[objectKey] = struct{}{}
		}
	}
	return existing, nil
}

func ExistingRawExchangeObjectKeys(objectKeys []string) (map[string]struct{}, error) {
	existing := make(map[string]struct{}, len(objectKeys))
	if len(objectKeys) == 0 {
		return existing, nil
	}
	var storedKeys []string
	if err := DB.Model(&RawExchangeArchive{}).
		Where("status IN ?", []string{
			RawExchangeStatusPendingCommit,
			RawExchangeStatusStored,
			RawExchangeStatusDeletePending,
			RawExchangeStatusDeleteFailed,
		}).
		Where("object_key IN ?", objectKeys).
		Pluck("object_key", &storedKeys).Error; err != nil {
		return nil, err
	}
	for _, objectKey := range storedKeys {
		existing[objectKey] = struct{}{}
	}
	return existing, nil
}

func ReconcileRawExchangeStorageUsage() (int64, error) {
	var adjustment int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var usage RawExchangeStorageUsage
		if err := lockForUpdate(tx).Where("id = ?", 1).First(&usage).Error; err != nil {
			return err
		}
		var expected struct {
			UsedBytes int64 `gorm:"column:used_bytes"`
		}
		if err := tx.Model(&RawExchangeArchive{}).
			Where("status IN ?", []string{RawExchangeStatusStored, RawExchangeStatusDeletePending, RawExchangeStatusDeleteFailed}).
			Where("storage_backend <> ? OR committed_at > 0", "s3").
			Select("COALESCE(SUM(request_stored_bytes + response_stored_bytes), 0) AS used_bytes").
			Scan(&expected).Error; err != nil {
			return err
		}
		adjustment = expected.UsedBytes - usage.UsedBytes
		if adjustment == 0 {
			return nil
		}
		return tx.Model(&RawExchangeStorageUsage{}).Where("id = ?", usage.Id).Updates(map[string]any{
			"used_bytes": expected.UsedBytes,
			"updated_at": common.GetTimestamp(),
		}).Error
	})
	return adjustment, err
}

func MarkRawExchangeMissing(archiveId int64, code string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var archive RawExchangeArchive
		if err := lockForUpdate(tx).Where("id = ?", archiveId).First(&archive).Error; err != nil {
			return err
		}
		if archive.Status != RawExchangeStatusStored {
			return ErrRawExchangeCommitStateChanged
		}
		storedBytes := archive.StoredBytes()
		if storedBytes > 0 {
			var usage RawExchangeStorageUsage
			if err := lockForUpdate(tx).Where("id = ?", 1).First(&usage).Error; err != nil {
				return err
			}
			usage.UsedBytes -= storedBytes
			if usage.UsedBytes < 0 {
				usage.UsedBytes = 0
			}
			usage.UpdatedAt = common.GetTimestamp()
			if err := tx.Save(&usage).Error; err != nil {
				return err
			}
		}
		return tx.Model(&RawExchangeArchive{}).Where("id = ?", archive.Id).Updates(map[string]any{
			"status":                RawExchangeStatusMissing,
			"request_available":     false,
			"response_available":    false,
			"request_stored_bytes":  int64(0),
			"response_stored_bytes": int64(0),
			"error_code":            code,
		}).Error
	})
}

func PurgeRawExchangeTombstoneById(archiveId int64) (bool, error) {
	result := DB.Where("id = ? AND status = ?", archiveId, RawExchangeStatusDeleted).Delete(&RawExchangeArchive{})
	return result.RowsAffected > 0, result.Error
}

func ListExpiredRawExchangeTombstones(now, afterId int64, limit int) ([]RawExchangeArchive, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var archives []RawExchangeArchive
	err := DB.Where("id > ? AND status = ?", afterId, RawExchangeStatusDeleted).
		Where("expires_at > 0 AND expires_at <= ?", now).
		Order("id ASC").
		Limit(limit).
		Find(&archives).Error
	return archives, err
}

func ListRawExchangeGCCandidates(now int64, pendingBefore int64, afterId int64, limit int) ([]RawExchangeArchive, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var archives []RawExchangeArchive
	err := DB.Where("id > ? AND status <> ?", afterId, RawExchangeStatusDeleted).
		Where("(expires_at > 0 AND expires_at <= ?) OR (status = ? AND created_at <= ?) OR status IN ?", now, RawExchangeStatusPendingCommit, pendingBefore, []string{
			RawExchangeStatusDeletePending,
			RawExchangeStatusDeleteFailed,
			RawExchangeStatusMissing,
		}).
		Order("id ASC").
		Limit(limit).
		Find(&archives).Error
	return archives, err
}

func applyRawExchangeCleanupFilter(query *gorm.DB, filter RawExchangeCleanupFilter) *gorm.DB {
	if filter.AfterTimestamp > 0 {
		query = query.Where("created_at >= ?", filter.AfterTimestamp)
	}
	if filter.BeforeTimestamp > 0 {
		query = query.Where("created_at < ?", filter.BeforeTimestamp)
	}
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.TokenId > 0 {
		query = query.Where("token_id = ?", filter.TokenId)
	}
	if filter.CaptureMode != "" {
		query = query.Where("capture_mode = ?", filter.CaptureMode)
	}
	if filter.Outcome != "" {
		query = query.Where("outcome = ?", filter.Outcome)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	} else if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}
	return query
}
