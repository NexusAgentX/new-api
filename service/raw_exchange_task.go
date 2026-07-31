package service

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const rawExchangeCleanupBatchSize = 100

type rawExchangeCleanupPreviewEnvelope struct {
	Filter     model.RawExchangeCleanupFilter  `json:"filter"`
	Preview    model.RawExchangeCleanupPreview `json:"preview"`
	OperatorId int                             `json:"operator_id"`
	ExpiresAt  int64                           `json:"expires_at"`
}

type RawExchangeCleanupAuditContext struct {
	OperatorId       int    `json:"operator_id"`
	OperatorUsername string `json:"operator_username"`
	OperatorRole     int    `json:"operator_role"`
	AuthMethod       string `json:"auth_method"`
	Ip               string `json:"ip"`
}

type RawExchangeCleanupPayload struct {
	Filter    model.RawExchangeCleanupFilter `json:"filter"`
	BatchSize int                            `json:"batch_size"`
	Audit     RawExchangeCleanupAuditContext `json:"audit"`
}

type RawExchangeCleanupResult struct {
	Matched              int64 `json:"matched"`
	Processed            int64 `json:"processed"`
	Deleted              int64 `json:"deleted"`
	AlreadyDeleted       int64 `json:"already_deleted"`
	Skipped              int64 `json:"skipped"`
	Failed               int64 `json:"failed"`
	RequestBytesFreed    int64 `json:"request_bytes_freed"`
	ResponseBytesFreed   int64 `json:"response_bytes_freed"`
	TotalBytesFreed      int64 `json:"total_bytes_freed"`
	StaleSpoolDeleted    int   `json:"stale_spool_deleted,omitempty"`
	MissingMarked        int   `json:"missing_marked,omitempty"`
	OrphanObjectsDeleted int   `json:"orphan_objects_deleted,omitempty"`
	TombstonesPurged     int   `json:"tombstones_purged,omitempty"`
	UsageAdjustmentBytes int64 `json:"usage_adjustment_bytes,omitempty"`
	ReconcileFailed      int   `json:"reconcile_failed,omitempty"`
}

type rawExchangeCleanupHandler struct{}

func (rawExchangeCleanupHandler) Type() string { return model.SystemTaskTypeRawExchangeCleanup }

func (rawExchangeCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := RawExchangeCleanupPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishRawExchangeTask(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	if err := ValidateRawExchangeCleanupFilter(payload.Filter); err != nil {
		if finishRawExchangeTask(task, runnerID, model.SystemTaskStatusFailed, nil, err) == nil {
			recordRawExchangeCleanupCompletionAudit(task.TaskID, payload, model.SystemTaskStatusFailed, nil, err)
		}
		return
	}
	if payload.BatchSize <= 0 || payload.BatchSize > 500 {
		payload.BatchSize = rawExchangeCleanupBatchSize
	}
	result, err := runRawExchangeCleanup(ctx, task, runnerID, payload)
	if result != nil {
		result.TotalBytesFreed = result.RequestBytesFreed + result.ResponseBytesFreed
	}
	status := model.SystemTaskStatusSucceeded
	if err != nil {
		status = model.SystemTaskStatusFailed
	}
	if finishRawExchangeTask(task, runnerID, status, result, err) == nil {
		recordRawExchangeCleanupCompletionAudit(task.TaskID, payload, status, result, err)
	}
}

type rawExchangeGCHandler struct{}

func (rawExchangeGCHandler) Type() string { return model.SystemTaskTypeRawExchangeGC }

func (rawExchangeGCHandler) Enabled() bool {
	store, err := rawExchangeStoreSnapshot()
	return err == nil && store.ready() == nil
}

func (rawExchangeGCHandler) Interval() time.Duration {
	minutes := system_setting.GetRawExchangeSettings().GCIntervalMinutes
	if minutes <= 0 {
		minutes = 15
	}
	return time.Duration(minutes) * time.Minute
}

func (rawExchangeGCHandler) NewPayload() any { return nil }

func (rawExchangeGCHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	result := &RawExchangeCleanupResult{}
	reconcileRawExchangeStorage(ctx, result)
	progress := NewSystemTaskProgressReporter(task, runnerID)
	now := common.GetTimestamp()
	pendingMinutes := system_setting.GetRawExchangeSettings().PendingTTLMinutes
	if pendingMinutes <= 0 {
		pendingMinutes = 60
	}
	pendingBefore := now - int64(time.Duration(pendingMinutes)*time.Minute/time.Second)
	var afterId int64
	for {
		if err := ctx.Err(); err != nil {
			finishRawExchangeTask(task, runnerID, model.SystemTaskStatusFailed, result, err)
			return
		}
		archives, err := model.ListRawExchangeGCCandidates(now, pendingBefore, afterId, rawExchangeCleanupBatchSize)
		if err != nil {
			finishRawExchangeTask(task, runnerID, model.SystemTaskStatusFailed, result, err)
			return
		}
		if len(archives) == 0 {
			break
		}
		for i := range archives {
			afterId = archives[i].Id
			result.Matched++
			alreadyDeleted, requestBytes, responseBytes, deleteErr := DeleteRawExchangeById(archives[i].Id)
			applyRawExchangeDeleteResult(result, alreadyDeleted, requestBytes, responseBytes, deleteErr)
			result.Processed++
			progress(int(result.Processed), int(result.Matched))
		}
	}
	afterId = 0
	for {
		if err := ctx.Err(); err != nil {
			finishRawExchangeTask(task, runnerID, model.SystemTaskStatusFailed, result, err)
			return
		}
		tombstones, err := model.ListExpiredRawExchangeTombstones(now, afterId, rawExchangeCleanupBatchSize)
		if err != nil {
			finishRawExchangeTask(task, runnerID, model.SystemTaskStatusFailed, result, err)
			return
		}
		if len(tombstones) == 0 {
			break
		}
		for i := range tombstones {
			afterId = tombstones[i].Id
			purged, purgeErr := model.PurgeRawExchangeTombstoneById(tombstones[i].Id)
			if purgeErr != nil {
				result.Failed++
				continue
			}
			if purged {
				result.TombstonesPurged++
			}
		}
	}
	if store, err := rawExchangeStoreSnapshot(); err == nil {
		deleted, cleanupErr := store.cleanupStaleSpool(time.Now().Add(-time.Duration(pendingMinutes) * time.Minute))
		if cleanupErr != nil {
			logger.LogWarn(ctx, "raw exchange stale spool cleanup failed: "+cleanupErr.Error())
		} else {
			result.StaleSpoolDeleted = deleted
		}
	}
	adjustment, adjustmentErr := model.ReconcileRawExchangeStorageUsage()
	if adjustmentErr != nil {
		result.ReconcileFailed++
		logger.LogWarn(ctx, "raw exchange usage reconciliation failed: "+adjustmentErr.Error())
	} else {
		result.UsageAdjustmentBytes = adjustment
	}
	result.TotalBytesFreed = result.RequestBytesFreed + result.ResponseBytesFreed
	progress(int(result.Processed), int(result.Processed))
	finishRawExchangeTask(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}

func init() {
	RegisterSystemTaskHandler(rawExchangeCleanupHandler{})
	RegisterSystemTaskHandler(rawExchangeGCHandler{})
}

func CreateRawExchangeCleanupPreview(filter model.RawExchangeCleanupFilter, operatorId int) (model.RawExchangeCleanupPreview, string, int64, error) {
	if operatorId <= 0 {
		return model.RawExchangeCleanupPreview{}, "", 0, errors.New("invalid cleanup operator")
	}
	if err := ValidateRawExchangeCleanupFilter(filter); err != nil {
		return model.RawExchangeCleanupPreview{}, "", 0, err
	}
	preview, err := model.PreviewRawExchangeCleanup(filter)
	if err != nil {
		return model.RawExchangeCleanupPreview{}, "", 0, err
	}
	expiresAt := common.GetTimestamp() + 600
	payload, err := common.Marshal(rawExchangeCleanupPreviewEnvelope{
		Filter:     filter,
		Preview:    preview,
		OperatorId: operatorId,
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		return model.RawExchangeCleanupPreview{}, "", 0, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	token := encoded + "." + common.GenerateHMAC(encoded)
	return preview, token, expiresAt, nil
}

func VerifyRawExchangeCleanupPreview(token string, operatorId int) (model.RawExchangeCleanupFilter, model.RawExchangeCleanupPreview, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return model.RawExchangeCleanupFilter{}, model.RawExchangeCleanupPreview{}, errors.New("invalid raw exchange cleanup preview token")
	}
	expected := common.GenerateHMAC(parts[0])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return model.RawExchangeCleanupFilter{}, model.RawExchangeCleanupPreview{}, errors.New("invalid raw exchange cleanup preview token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return model.RawExchangeCleanupFilter{}, model.RawExchangeCleanupPreview{}, errors.New("invalid raw exchange cleanup preview token")
	}
	var envelope rawExchangeCleanupPreviewEnvelope
	if err := common.Unmarshal(payload, &envelope); err != nil {
		return model.RawExchangeCleanupFilter{}, model.RawExchangeCleanupPreview{}, errors.New("invalid raw exchange cleanup preview token")
	}
	if envelope.OperatorId != operatorId || envelope.ExpiresAt < common.GetTimestamp() {
		return model.RawExchangeCleanupFilter{}, model.RawExchangeCleanupPreview{}, errors.New("raw exchange cleanup preview token expired or belongs to another operator")
	}
	if err := ValidateRawExchangeCleanupFilter(envelope.Filter); err != nil {
		return model.RawExchangeCleanupFilter{}, model.RawExchangeCleanupPreview{}, err
	}
	return envelope.Filter, envelope.Preview, nil
}

func StartRawExchangeCleanupTask(filter model.RawExchangeCleanupFilter, audit RawExchangeCleanupAuditContext) (*model.SystemTask, bool, error) {
	if err := ValidateRawExchangeCleanupFilter(filter); err != nil {
		return nil, false, err
	}
	if audit.OperatorId <= 0 {
		return nil, false, errors.New("invalid raw exchange cleanup audit operator")
	}
	return EnqueueSystemTask(model.SystemTaskTypeRawExchangeCleanup, RawExchangeCleanupPayload{
		Filter:    filter,
		BatchSize: rawExchangeCleanupBatchSize,
		Audit:     audit,
	})
}

func ValidateRawExchangeCleanupFilter(filter model.RawExchangeCleanupFilter) error {
	if filter.AfterTimestamp < 0 || filter.BeforeTimestamp < 0 || filter.UserId < 0 || filter.TokenId < 0 {
		return errors.New("raw exchange cleanup filter contains a negative value")
	}
	if filter.AfterTimestamp > 0 && filter.BeforeTimestamp > 0 && filter.AfterTimestamp >= filter.BeforeTimestamp {
		return errors.New("raw exchange cleanup start time must be before the end time")
	}
	if filter.CaptureMode != "" {
		mode, err := model.NormalizeRawExchangeCaptureMode(filter.CaptureMode)
		if err != nil || mode == model.RawExchangeCaptureOff {
			return errors.New("invalid raw exchange capture mode filter")
		}
	}
	if filter.Outcome != "" && filter.Outcome != model.RawExchangeOutcomeSuccess && filter.Outcome != model.RawExchangeOutcomeNonSuccess {
		return errors.New("invalid raw exchange outcome filter")
	}
	if filter.Status != "" && !validRawExchangeStatus(filter.Status) {
		return errors.New("invalid raw exchange status filter")
	}
	if filter.Status != "" && len(filter.Statuses) > 0 {
		return errors.New("raw exchange status and statuses filters cannot be combined")
	}
	seenStatuses := make(map[string]struct{}, len(filter.Statuses))
	for _, status := range filter.Statuses {
		if !validRawExchangeStatus(status) {
			return errors.New("invalid raw exchange statuses filter")
		}
		if _, exists := seenStatuses[status]; exists {
			return errors.New("duplicate raw exchange status filter")
		}
		seenStatuses[status] = struct{}{}
	}
	return nil
}

func runRawExchangeCleanup(ctx context.Context, task *model.SystemTask, runnerID string, payload RawExchangeCleanupPayload) (*RawExchangeCleanupResult, error) {
	preview, err := model.PreviewRawExchangeCleanup(payload.Filter)
	if err != nil {
		return nil, err
	}
	result := &RawExchangeCleanupResult{Matched: preview.Count}
	progress := NewSystemTaskProgressReporter(task, runnerID)
	progress(0, int(preview.Count))
	var afterId int64
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		archives, err := model.ListRawExchangeCleanupCandidates(payload.Filter, afterId, payload.BatchSize)
		if err != nil {
			return result, err
		}
		if len(archives) == 0 {
			break
		}
		for i := range archives {
			afterId = archives[i].Id
			if archives[i].Status == model.RawExchangeStatusDeleted {
				purged, purgeErr := model.PurgeRawExchangeTombstoneById(archives[i].Id)
				if purgeErr != nil {
					result.Failed++
				} else if purged {
					result.Deleted++
					result.TombstonesPurged++
				} else {
					result.AlreadyDeleted++
				}
			} else {
				alreadyDeleted, requestBytes, responseBytes, deleteErr := DeleteRawExchangeById(archives[i].Id)
				applyRawExchangeDeleteResult(result, alreadyDeleted, requestBytes, responseBytes, deleteErr)
			}
			result.Processed++
			progress(int(result.Processed), int(preview.Count))
		}
	}
	result.TotalBytesFreed = result.RequestBytesFreed + result.ResponseBytesFreed
	progress(int(result.Processed), int(result.Processed))
	return result, nil
}

func applyRawExchangeDeleteResult(result *RawExchangeCleanupResult, alreadyDeleted bool, requestBytes int64, responseBytes int64, err error) {
	if err != nil {
		if errors.Is(err, model.ErrRawExchangeDownloadInProgress) || errors.Is(err, model.ErrRawExchangeDeletionInProgress) || errors.Is(err, ErrRawExchangeDeletionDeferred) {
			result.Skipped++
		} else {
			result.Failed++
		}
		return
	}
	if alreadyDeleted {
		result.AlreadyDeleted++
		return
	}
	result.Deleted++
	result.RequestBytesFreed += requestBytes
	result.ResponseBytesFreed += responseBytes
}

func validRawExchangeStatus(status string) bool {
	switch status {
	case model.RawExchangeStatusPendingCommit,
		model.RawExchangeStatusStored,
		model.RawExchangeStatusSkippedTooLarge,
		model.RawExchangeStatusSkippedCapacity,
		model.RawExchangeStatusCaptureFailed,
		model.RawExchangeStatusDeletePending,
		model.RawExchangeStatusDeleteFailed,
		model.RawExchangeStatusDeleted,
		model.RawExchangeStatusMissing:
		return true
	default:
		return false
	}
}

func recordRawExchangeCleanupCompletionAudit(taskId string, payload RawExchangeCleanupPayload, status model.SystemTaskStatus, result *RawExchangeCleanupResult, runErr error) {
	if payload.Audit.OperatorId <= 0 {
		return
	}
	params := map[string]interface{}{
		"filter":          payload.Filter,
		"task_id":         taskId,
		"status":          string(status),
		"matched":         int64(0),
		"processed":       int64(0),
		"deleted":         int64(0),
		"already_deleted": int64(0),
		"skipped":         int64(0),
		"failed":          int64(0),
		"request_bytes":   int64(0),
		"response_bytes":  int64(0),
		"total_bytes":     int64(0),
		"error_code":      "",
	}
	if result != nil {
		params["matched"] = result.Matched
		params["processed"] = result.Processed
		params["deleted"] = result.Deleted
		params["already_deleted"] = result.AlreadyDeleted
		params["skipped"] = result.Skipped
		params["failed"] = result.Failed
		params["request_bytes"] = result.RequestBytesFreed
		params["response_bytes"] = result.ResponseBytesFreed
		params["total_bytes"] = result.TotalBytesFreed
	}
	if runErr != nil {
		params["error_code"] = "cleanup_failed"
	}
	content := fmt.Sprintf("Completed raw exchange cleanup task %s with status %s: %v deleted, %v failed", taskId, status, params["deleted"], params["failed"])
	adminInfo := map[string]interface{}{
		"admin_id":       payload.Audit.OperatorId,
		"admin_username": payload.Audit.OperatorUsername,
		"admin_role":     payload.Audit.OperatorRole,
		"auth_method":    payload.Audit.AuthMethod,
	}
	model.RecordOperationAuditLog(payload.Audit.OperatorId, content, payload.Audit.Ip, "raw_exchange.cleanup_complete", params, adminInfo, nil)
}

func finishRawExchangeTask(task *model.SystemTask, runnerID string, status model.SystemTaskStatus, result any, runErr error) error {
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("raw exchange task %s failed to persist result: %v", task.TaskID, err))
		return err
	}
	return nil
}
