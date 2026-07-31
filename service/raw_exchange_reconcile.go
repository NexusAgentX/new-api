package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const rawExchangeReconcileBatchSize = 500

func reconcileRawExchangeStorage(ctx context.Context, result *RawExchangeCleanupResult) {
	store, err := rawExchangeStoreSnapshot()
	if err != nil {
		return
	}
	localShared, err := rawExchangeLocalStorageShared()
	if err != nil {
		result.ReconcileFailed++
		logger.LogWarn(ctx, "raw exchange reconcile configuration failed: "+err.Error())
		return
	}
	if !reconcileRawExchangeMetadata(ctx, store, localShared, result) {
		return
	}
	reconcileRawExchangeOrphans(ctx, store, result)
}

func reconcileRawExchangeMetadata(ctx context.Context, store rawExchangeStore, localShared bool, result *RawExchangeCleanupResult) bool {
	var afterId int64
	for {
		if ctx.Err() != nil {
			return false
		}
		archives, err := model.ListStoredRawExchangeArchives(
			store.backend(),
			truncateRawExchangeMetadata(common.NodeName, 128),
			localShared,
			afterId,
			rawExchangeReconcileBatchSize,
		)
		if err != nil {
			result.ReconcileFailed++
			logger.LogWarn(ctx, "raw exchange metadata reconcile query failed: "+err.Error())
			return false
		}
		if len(archives) == 0 {
			return true
		}
		for i := range archives {
			afterId = archives[i].Id
			parts := []string{rawExchangeManifestFileName}
			if archives[i].RequestAvailable {
				parts = append(parts, rawExchangeRequestFileName)
			}
			if archives[i].ResponseAvailable {
				parts = append(parts, rawExchangeResponseFileName)
			}
			missingPart := ""
			for _, part := range parts {
				exists, err := store.partExists(archives[i].ObjectKey, part)
				if err != nil {
					result.ReconcileFailed++
					logger.LogWarn(ctx, "raw exchange object reconcile failed: "+err.Error())
					return false
				}
				if !exists {
					missingPart = part
					break
				}
			}
			if missingPart == "" {
				continue
			}
			if err := model.MarkRawExchangeMissing(archives[i].Id, "object_missing"); err != nil {
				result.ReconcileFailed++
				logger.LogWarn(ctx, fmt.Sprintf("raw exchange missing state update failed for archive %d: %v", archives[i].Id, err))
				continue
			}
			result.MissingMarked++
		}
	}
}

func reconcileRawExchangeOrphans(ctx context.Context, store rawExchangeStore, result *RawExchangeCleanupResult) {
	pendingMinutes := system_setting.GetRawExchangeSettings().PendingTTLMinutes
	if pendingMinutes <= 0 {
		pendingMinutes = 60
	}
	orphanBefore := time.Now().Add(-time.Duration(pendingMinutes) * time.Minute)
	cursor := ""
	for {
		if ctx.Err() != nil {
			return
		}
		objects, nextCursor, done, err := store.scanCommittedObjects(cursor, rawExchangeReconcileBatchSize)
		if err != nil {
			result.ReconcileFailed++
			logger.LogWarn(ctx, "raw exchange orphan scan failed: "+err.Error())
			return
		}
		eligible := make([]rawExchangeCommittedObject, 0, len(objects))
		keys := make([]string, 0, len(objects))
		for _, object := range objects {
			if object.modifiedAt.IsZero() || !object.modifiedAt.Before(orphanBefore) {
				continue
			}
			eligible = append(eligible, object)
			keys = append(keys, object.objectKey)
		}
		existing, err := model.ExistingRawExchangeObjectKeys(keys)
		if err != nil {
			result.ReconcileFailed++
			logger.LogWarn(ctx, "raw exchange orphan metadata query failed: "+err.Error())
			return
		}
		for _, object := range eligible {
			if _, exists := existing[object.objectKey]; exists {
				continue
			}
			if err := store.delete(object.objectKey); err != nil {
				result.ReconcileFailed++
				logger.LogWarn(ctx, "raw exchange orphan delete failed: "+err.Error())
				continue
			}
			result.OrphanObjectsDeleted++
		}
		if done {
			return
		}
		cursor = nextCursor
		if cursor == "" {
			result.ReconcileFailed++
			logger.LogWarn(ctx, "raw exchange orphan scan returned an empty continuation cursor")
			return
		}
	}
}
