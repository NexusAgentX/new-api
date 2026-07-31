package service

import (
	"errors"
	"io"
	"os"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type RawExchangeDownload struct {
	archive *model.RawExchangeArchive
	leaseId string
	store   rawExchangeStore
	once    sync.Once
}

var ErrRawExchangeDeletionDeferred = errors.New("raw exchange deletion is pending on the owning node")

type RawExchangeDeleteResult struct {
	Selected           int   `json:"selected"`
	Deleted            int   `json:"deleted"`
	Pending            int   `json:"pending"`
	AlreadyDeleted     int   `json:"already_deleted"`
	Failed             int   `json:"failed"`
	RequestBytesFreed  int64 `json:"request_bytes_freed"`
	ResponseBytesFreed int64 `json:"response_bytes_freed"`
	TotalBytesFreed    int64 `json:"total_bytes_freed"`
}

func AcquireRawExchangeDownload(requestId string, userId int) (*RawExchangeDownload, error) {
	archive, leaseId, err := model.AcquireRawExchangeDownload(requestId, userId, 300)
	if err != nil {
		return nil, err
	}
	store, err := rawExchangeStoreSnapshot()
	if err == nil {
		err = store.ready()
	}
	if err != nil {
		_ = model.ReleaseRawExchangeDownload(leaseId)
		return nil, err
	}
	if archive.StorageBackend != store.backend() {
		_ = model.ReleaseRawExchangeDownload(leaseId)
		return nil, errors.New("raw exchange storage backend is unavailable")
	}
	if archive.StorageBackend == rawExchangeStorageBackendS3 {
		exists, partErr := store.partExists(archive.ObjectKey, rawExchangeManifestFileName)
		if partErr != nil {
			_ = model.ReleaseRawExchangeDownload(leaseId)
			return nil, partErr
		}
		if !exists {
			_ = model.ReleaseRawExchangeDownload(leaseId)
			_ = model.MarkRawExchangeMissing(archive.Id, "manifest_missing")
			return nil, model.ErrRawExchangeNotStored
		}
	}
	return &RawExchangeDownload{archive: archive, leaseId: leaseId, store: store}, nil
}

func (d *RawExchangeDownload) Archive() *model.RawExchangeArchive {
	if d == nil {
		return nil
	}
	return d.archive
}

func (d *RawExchangeDownload) OpenRequest() (io.ReadCloser, int64, error) {
	if d == nil || d.archive == nil || !d.archive.RequestAvailable {
		return nil, 0, model.ErrRawExchangeNotStored
	}
	return d.openPart(rawExchangeRequestFileName)
}

func (d *RawExchangeDownload) OpenResponse() (io.ReadCloser, int64, error) {
	if d == nil || d.archive == nil || !d.archive.ResponseAvailable {
		return nil, 0, model.ErrRawExchangeNotStored
	}
	return d.openPart(rawExchangeResponseFileName)
}

func (d *RawExchangeDownload) openPart(part string) (io.ReadCloser, int64, error) {
	reader, size, err := d.store.open(d.archive.ObjectKey, part)
	if !isRawExchangeObjectNotFound(err) {
		return reader, size, err
	}
	_ = model.MarkRawExchangeMissing(d.archive.Id, "object_missing")
	return nil, 0, model.ErrRawExchangeNotStored
}

func (d *RawExchangeDownload) Close() error {
	if d == nil {
		return nil
	}
	var err error
	d.once.Do(func() {
		err = model.ReleaseRawExchangeDownload(d.leaseId)
	})
	return err
}

func DeleteRawExchangeForUser(requestId string, userId int) (bool, int64, int64, error) {
	archive, alreadyDeleted, err := model.BeginRawExchangeDelete(requestId, userId)
	if err != nil || alreadyDeleted {
		return alreadyDeleted, 0, 0, err
	}
	requestBytes, responseBytes, err := deletePreparedRawExchange(archive)
	return false, requestBytes, responseBytes, err
}

func DeleteRawExchangeById(archiveId int64) (bool, int64, int64, error) {
	archive, alreadyDeleted, err := model.PrepareRawExchangeDeleteById(archiveId)
	if err != nil || alreadyDeleted {
		return alreadyDeleted, 0, 0, err
	}
	requestBytes, responseBytes, err := deletePreparedRawExchange(archive)
	return false, requestBytes, responseBytes, err
}

func deletePreparedRawExchange(archive *model.RawExchangeArchive) (int64, int64, error) {
	if archive == nil {
		return 0, 0, errors.New("raw exchange archive is nil")
	}
	requestBytes := archive.RequestStoredBytes
	responseBytes := archive.ResponseStoredBytes
	if archive.ObjectKey != "" {
		if archive.StorageBackend == rawExchangeStorageBackendS3 && archive.CommittedAt == 0 && archive.NodeName != truncateRawExchangeMetadata(common.NodeName, 128) {
			return 0, 0, ErrRawExchangeDeletionDeferred
		}
		store, storeErr := rawExchangeStoreSnapshot()
		if storeErr == nil {
			storeErr = store.ready()
		}
		if storeErr != nil {
			_ = model.MarkRawExchangeDeleteFailed(archive.Id, "storage_unavailable")
			return 0, 0, storeErr
		}
		if archive.StorageBackend != store.backend() {
			_ = model.MarkRawExchangeDeleteFailed(archive.Id, "backend_unavailable")
			return 0, 0, errors.New("raw exchange storage backend is unavailable")
		}
		var staging *rawExchangeStaging
		if archive.StorageBackend == rawExchangeStorageBackendS3 && archive.CommittedAt == 0 {
			staging, storeErr = store.stagingForObjectKey(archive.ObjectKey)
			if storeErr != nil && !errors.Is(storeErr, os.ErrNotExist) {
				_ = model.MarkRawExchangeDeleteFailed(archive.Id, "spool_access_failed")
				return 0, 0, storeErr
			}
		}
		if deleteErr := store.delete(archive.ObjectKey); deleteErr != nil {
			_ = model.MarkRawExchangeDeleteFailed(archive.Id, "object_delete_failed")
			return 0, 0, deleteErr
		}
		if staging != nil {
			if deleteErr := removeRawExchangeStaging(staging); deleteErr != nil {
				_ = model.MarkRawExchangeDeleteFailed(archive.Id, "spool_delete_failed")
				return 0, 0, deleteErr
			}
		}
	}
	if err := model.CompleteRawExchangeDelete(archive.Id); err != nil {
		_ = model.MarkRawExchangeDeleteFailed(archive.Id, "metadata_delete_failed")
		return 0, 0, err
	}
	return requestBytes, responseBytes, nil
}

func DeleteRawExchangeBatchForUser(requestIds []string, userId int) RawExchangeDeleteResult {
	result := RawExchangeDeleteResult{Selected: len(requestIds)}
	seen := make(map[string]struct{}, len(requestIds))
	for _, requestId := range requestIds {
		if requestId == "" || len(requestId) > 64 {
			result.Failed++
			continue
		}
		if _, exists := seen[requestId]; exists {
			result.Failed++
			continue
		}
		seen[requestId] = struct{}{}
		alreadyDeleted, requestBytes, responseBytes, err := DeleteRawExchangeForUser(requestId, userId)
		if errors.Is(err, ErrRawExchangeDeletionDeferred) {
			result.Pending++
			continue
		}
		if err != nil {
			result.Failed++
			continue
		}
		if alreadyDeleted {
			result.AlreadyDeleted++
			continue
		}
		result.Deleted++
		result.RequestBytesFreed += requestBytes
		result.ResponseBytesFreed += responseBytes
	}
	result.TotalBytesFreed = result.RequestBytesFreed + result.ResponseBytesFreed
	return result
}
