package service

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	rawExchangeCommitQueueSize = 256
	rawExchangeCommitBatchSize = 100
)

var rawExchangeCommitWorkerState struct {
	sync.Once
	queue chan int64
}

func init() {
	rawExchangeCommitWorkerState.queue = make(chan int64, rawExchangeCommitQueueSize)
}

func StartRawExchangeCommitWorker() {
	store, err := rawExchangeStoreSnapshot()
	if err != nil || store.backend() != rawExchangeStorageBackendS3 {
		return
	}
	rawExchangeCommitWorkerState.Do(func() {
		go runRawExchangeCommitWorker()
	})
}

func enqueueRawExchangeCommit(archiveId int64) {
	if archiveId <= 0 {
		return
	}
	select {
	case rawExchangeCommitWorkerState.queue <- archiveId:
	default:
		// The periodic pending scan recovers work when the in-memory queue is full.
	}
}

func runRawExchangeCommitWorker() {
	processPendingRawExchangeCommits()
	processPendingRawExchangeDeletes()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case archiveId := <-rawExchangeCommitWorkerState.queue:
			commitPendingRawExchangeById(archiveId)
		case <-ticker.C:
			processPendingRawExchangeCommits()
			processPendingRawExchangeDeletes()
		}
	}
}

func processPendingRawExchangeCommits() {
	nodeName := truncateRawExchangeMetadata(common.NodeName, 128)
	archives, err := model.ListPendingRawExchangeCommits(nodeName, rawExchangeCommitBatchSize)
	if err != nil {
		logger.LogWarn(nil, "raw exchange pending commit scan failed: "+err.Error())
		return
	}
	for i := range archives {
		commitPendingRawExchange(&archives[i])
	}
}

func processPendingRawExchangeDeletes() {
	nodeName := truncateRawExchangeMetadata(common.NodeName, 128)
	archives, err := model.ListPendingRawExchangeDeletes(nodeName, rawExchangeCommitBatchSize)
	if err != nil {
		logger.LogWarn(nil, "raw exchange pending delete scan failed: "+err.Error())
		return
	}
	for i := range archives {
		archive := &archives[i]
		if archive.Status == model.RawExchangeStatusDeleteFailed {
			prepared, alreadyDeleted, err := model.PrepareRawExchangeDeleteById(archive.Id)
			if err != nil || alreadyDeleted {
				continue
			}
			archive = prepared
		}
		if _, _, err := deletePreparedRawExchange(archive); err != nil && !errors.Is(err, model.ErrRawExchangeDeletionInProgress) {
			logger.LogWarn(nil, "raw exchange pending delete failed: "+err.Error())
		}
	}
}

func commitPendingRawExchangeById(archiveId int64) {
	archive, err := model.GetRawExchangeArchiveById(archiveId)
	if err != nil {
		logger.LogWarn(nil, "raw exchange pending commit lookup failed: "+err.Error())
		return
	}
	commitPendingRawExchange(archive)
}

func commitPendingRawExchange(archive *model.RawExchangeArchive) {
	if archive == nil || archive.Status != model.RawExchangeStatusPendingCommit || archive.StorageBackend != rawExchangeStorageBackendS3 {
		return
	}
	if archive.NextCommitAt > common.GetTimestamp() {
		return
	}
	store, err := rawExchangeStoreSnapshot()
	if err != nil || store.backend() != archive.StorageBackend {
		_ = model.UpdateRawExchangePendingCommitError(archive.Id, "storage_unavailable")
		return
	}
	staging, err := store.stagingForObjectKey(archive.ObjectKey)
	if err != nil {
		handleRawExchangeCommitFailure(store, nil, archive, "spool_missing", err)
		return
	}

	committedAt := common.GetTimestamp()
	manifestArchive := *archive
	manifestArchive.Status = model.RawExchangeStatusStored
	manifestArchive.CommittedAt = committedAt
	manifestArchive.ErrorCode = ""
	if err := replaceRawExchangeManifest(staging, &manifestArchive); err != nil {
		handleRawExchangeCommitFailure(store, staging, archive, "manifest_write_failed", err)
		return
	}
	if err := store.commit(staging, archive.ObjectKey); err != nil {
		handleRawExchangeCommitFailure(store, staging, archive, "object_commit_failed", err)
		return
	}

	capacityBytes := positiveRawExchangeLimit(system_setting.GetRawExchangeSettings().GlobalCapacityBytes, 10<<30)
	err = model.CompleteRawExchangeCommit(archive.Id, committedAt, capacityBytes)
	switch {
	case err == nil:
		store.abort(staging)
	case errors.Is(err, model.ErrRawExchangeCapacityExceeded):
		if deleteErr := store.delete(archive.ObjectKey); deleteErr != nil {
			_ = model.UpdateRawExchangePendingCommitError(archive.Id, "capacity_cleanup_failed")
			logger.LogWarn(nil, "raw exchange S3 capacity cleanup failed: "+deleteErr.Error())
			return
		}
		if failErr := model.FailRawExchangePendingCommit(archive.Id, model.RawExchangeStatusSkippedCapacity, "capacity_exceeded"); failErr != nil {
			logger.LogWarn(nil, "raw exchange capacity failure state update failed: "+failErr.Error())
			return
		}
		store.abort(staging)
	case errors.Is(err, model.ErrRawExchangeCommitStateChanged):
		if deleteErr := store.delete(archive.ObjectKey); deleteErr != nil {
			logger.LogWarn(nil, "raw exchange stale S3 commit cleanup failed: "+deleteErr.Error())
			return
		}
		store.abort(staging)
	default:
		_ = model.UpdateRawExchangePendingCommitError(archive.Id, "metadata_commit_failed")
		logger.LogWarn(nil, "raw exchange metadata commit failed: "+err.Error())
	}
}

func replaceRawExchangeManifest(staging *rawExchangeStaging, archive *model.RawExchangeArchive) error {
	manifestData, err := common.Marshal(rawExchangeManifest{Version: 1, ObjectKey: archive.ObjectKey, Archive: archive})
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(staging.path, rawExchangeManifestFileName)
	file, err := os.CreateTemp(staging.path, "manifest-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(temporaryPath)
		return err
	}
	written, writeErr := file.Write(manifestData)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || written != len(manifestData) {
		_ = os.Remove(temporaryPath)
		return errors.Join(writeErr, syncErr, closeErr)
	}
	if err := os.Rename(temporaryPath, manifestPath); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	return nil
}

func handleRawExchangeCommitFailure(store rawExchangeStore, staging *rawExchangeStaging, archive *model.RawExchangeArchive, code string, cause error) {
	pendingMinutes := system_setting.GetRawExchangeSettings().PendingTTLMinutes
	if pendingMinutes <= 0 {
		pendingMinutes = 60
	}
	if archive.CreatedAt > time.Now().Add(-time.Duration(pendingMinutes)*time.Minute).Unix() {
		_ = model.UpdateRawExchangePendingCommitError(archive.Id, code)
		logger.LogWarn(nil, "raw exchange S3 commit will retry: "+cause.Error())
		return
	}
	if archive.ObjectKey != "" {
		if err := store.delete(archive.ObjectKey); err != nil {
			_ = model.UpdateRawExchangePendingCommitError(archive.Id, "object_cleanup_failed")
			logger.LogWarn(nil, "raw exchange expired S3 commit cleanup failed: "+err.Error())
			return
		}
	}
	if err := model.FailRawExchangePendingCommit(archive.Id, model.RawExchangeStatusCaptureFailed, code); err != nil {
		logger.LogWarn(nil, "raw exchange expired commit state update failed: "+err.Error())
		return
	}
	store.abort(staging)
}
