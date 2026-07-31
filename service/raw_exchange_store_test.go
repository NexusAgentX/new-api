package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fakeRawExchangeS3 struct {
	objects               map[string][]byte
	modifiedAt            map[string]time.Time
	putOrder              []string
	putSSE                []s3types.ServerSideEncryption
	putErrorsRemaining    int
	putErrorAtCall        int
	nextPutError          error
	putCalls              int
	deleteOrder           []string
	deleteErrorsRemaining int
}

func newFakeRawExchangeS3() *fakeRawExchangeS3 {
	return &fakeRawExchangeS3{objects: make(map[string][]byte), modifiedAt: make(map[string]time.Time)}
}

func (f *fakeRawExchangeS3) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, nil
}

func (f *fakeRawExchangeS3) GetBucketEncryption(context.Context, *s3.GetBucketEncryptionInput, ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
	return &s3.GetBucketEncryptionOutput{
		ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
			Rules: []s3types.ServerSideEncryptionRule{{
				ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{SSEAlgorithm: s3types.ServerSideEncryptionAes256},
			}},
		},
	}, nil
}

func (f *fakeRawExchangeS3) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	data, exists := f.objects[aws.ToString(input.Key)]
	if !exists {
		return nil, os.ErrNotExist
	}
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(int64(len(data)))}, nil
}

func (f *fakeRawExchangeS3) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.putCalls++
	if f.nextPutError != nil {
		err := f.nextPutError
		f.nextPutError = nil
		return nil, err
	}
	if f.putErrorAtCall > 0 && f.putCalls == f.putErrorAtCall {
		return nil, errors.New("injected partial S3 upload failure")
	}
	if f.putErrorsRemaining > 0 {
		f.putErrorsRemaining--
		return nil, errors.New("injected S3 upload failure")
	}
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	key := aws.ToString(input.Key)
	f.objects[key] = data
	f.modifiedAt[key] = time.Now()
	f.putOrder = append(f.putOrder, key)
	f.putSSE = append(f.putSSE, input.ServerSideEncryption)
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeRawExchangeS3) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	data, exists := f.objects[aws.ToString(input.Key)]
	if !exists {
		return nil, os.ErrNotExist
	}
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: aws.Int64(int64(len(data))),
	}, nil
}

func (f *fakeRawExchangeS3) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if f.deleteErrorsRemaining > 0 {
		f.deleteErrorsRemaining--
		return nil, errors.New("injected S3 delete failure")
	}
	key := aws.ToString(input.Key)
	delete(f.objects, key)
	delete(f.modifiedAt, key)
	f.deleteOrder = append(f.deleteOrder, key)
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeRawExchangeS3) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		if strings.HasPrefix(key, aws.ToString(input.Prefix)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	objects := make([]s3types.Object, 0, len(keys))
	for _, key := range keys {
		modifiedAt := f.modifiedAt[key]
		objects = append(objects, s3types.Object{Key: aws.String(key), LastModified: &modifiedAt})
	}
	return &s3.ListObjectsV2Output{Contents: objects, IsTruncated: aws.Bool(false)}, nil
}

func setupRawExchangeS3ServiceTest(t *testing.T) (*gorm.DB, *rawExchangeS3Store, *fakeRawExchangeS3) {
	t.Helper()
	previousDB := model.DB
	previousMainType := common.MainDatabaseType()
	previousLogType := common.LogDatabaseType()
	previousSettings := *system_setting.GetRawExchangeSettings()

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.RawExchangeArchive{}, &model.RawExchangeStorageUsage{}, &model.RawExchangeDownloadLease{}))
	require.NoError(t, model.InitializeRawExchangeStorageUsage())

	spool, err := initRawExchangeSpool(t.TempDir())
	require.NoError(t, err)
	client := newFakeRawExchangeS3()
	store := &rawExchangeS3Store{
		spool:                spool,
		client:               client,
		bucket:               "private-archives",
		prefix:               "raw-exchanges",
		serverSideEncryption: s3types.ServerSideEncryptionAes256,
	}
	rawExchangeStoreState.Lock()
	previousStore := rawExchangeStoreState.store
	previousInitErr := rawExchangeStoreState.initErr
	rawExchangeStoreState.store = store
	rawExchangeStoreState.initErr = nil
	rawExchangeStoreState.Unlock()

	settings := system_setting.GetRawExchangeSettings()
	settings.GlobalCapacityBytes = 1 << 20
	settings.PendingTTLMinutes = 60

	t.Cleanup(func() {
		rawExchangeStoreState.Lock()
		rawExchangeStoreState.store = previousStore
		rawExchangeStoreState.initErr = previousInitErr
		rawExchangeStoreState.Unlock()
		*system_setting.GetRawExchangeSettings() = previousSettings
		model.DB = previousDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db, store, client
}

func setupRawExchangeLocalServiceTest(t *testing.T) (*gorm.DB, *rawExchangeLocalStore) {
	t.Helper()
	previousDB := model.DB
	previousMainType := common.MainDatabaseType()
	previousLogType := common.LogDatabaseType()
	previousSettings := *system_setting.GetRawExchangeSettings()
	previousShared := os.Getenv("RAW_EXCHANGE_LOCAL_SHARED")

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.RawExchangeArchive{}, &model.RawExchangeStorageUsage{}, &model.RawExchangeDownloadLease{}))
	require.NoError(t, model.InitializeRawExchangeStorageUsage())

	root := t.TempDir()
	spool, err := initRawExchangeSpool(root)
	require.NoError(t, err)
	objectsDir := path.Join(root, "objects")
	require.NoError(t, ensurePrivateRawExchangeDir(objectsDir))
	store := &rawExchangeLocalStore{spool: spool, objectsDir: objectsDir}
	rawExchangeStoreState.Lock()
	previousStore := rawExchangeStoreState.store
	previousInitErr := rawExchangeStoreState.initErr
	rawExchangeStoreState.store = store
	rawExchangeStoreState.initErr = nil
	rawExchangeStoreState.Unlock()
	require.NoError(t, os.Unsetenv("RAW_EXCHANGE_LOCAL_SHARED"))

	settings := system_setting.GetRawExchangeSettings()
	settings.PendingTTLMinutes = 60

	t.Cleanup(func() {
		rawExchangeStoreState.Lock()
		rawExchangeStoreState.store = previousStore
		rawExchangeStoreState.initErr = previousInitErr
		rawExchangeStoreState.Unlock()
		_ = os.Setenv("RAW_EXCHANGE_LOCAL_SHARED", previousShared)
		*system_setting.GetRawExchangeSettings() = previousSettings
		model.DB = previousDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db, store
}

func writeRawExchangeTestPart(t *testing.T, store rawExchangeStore, staging *rawExchangeStaging, name string, data []byte) {
	t.Helper()
	file, err := store.createPart(staging, name)
	require.NoError(t, err)
	_, err = file.Write(data)
	require.NoError(t, err)
	require.NoError(t, file.Sync())
	require.NoError(t, file.Close())
}

func createPendingRawExchangeS3Archive(t *testing.T, store *rawExchangeS3Store, requestId string) (*model.RawExchangeArchive, *rawExchangeStaging) {
	t.Helper()
	requestBody := []byte(`{"model":"gpt-test"}`)
	responseBody := []byte(`{"error":"upstream failed"}`)
	staging, err := store.begin()
	require.NoError(t, err)
	writeRawExchangeTestPart(t, store, staging, rawExchangeRequestFileName, requestBody)
	writeRawExchangeTestPart(t, store, staging, rawExchangeResponseFileName, responseBody)
	objectKey, err := store.objectKey(staging, time.Now())
	require.NoError(t, err)
	archive := &model.RawExchangeArchive{
		RequestId:           requestId,
		UserId:              7,
		TokenId:             8,
		Status:              model.RawExchangeStatusPendingCommit,
		StorageBackend:      rawExchangeStorageBackendS3,
		ObjectKey:           objectKey,
		NodeName:            truncateRawExchangeMetadata(common.NodeName, 128),
		RequestAvailable:    true,
		ResponseAvailable:   true,
		RequestStoredBytes:  int64(len(requestBody)),
		ResponseStoredBytes: int64(len(responseBody)),
		CreatedAt:           time.Now().Unix(),
	}
	require.NoError(t, writeRawExchangeManifest(store, staging, archive))
	require.NoError(t, model.CreateRawExchangeArchive(archive, 1<<20))
	return archive, staging
}

func TestTruncateRawExchangeMetadataPreservesUTF8(t *testing.T) {
	assert.Equal(t, "你", truncateRawExchangeMetadata("你好", 4))
	assert.Equal(t, "valid", truncateRawExchangeMetadata("valid\xff", 10))
}

func TestRawExchangeCaptureTreatsShortWriteAsIncomplete(t *testing.T) {
	_, store := setupRawExchangeLocalServiceTest(t)
	staging, err := store.begin()
	require.NoError(t, err)
	session := &RawExchangeCaptureSession{
		store:            store,
		staging:          staging,
		maxResponseBytes: 1 << 20,
		maxExchangeBytes: 1 << 20,
	}

	session.captureResponse([]byte("partial"), 3, nil)
	assert.True(t, session.writeFailed)
	assert.Equal(t, int64(3), session.responseBytes)
	assert.Equal(t, int64(3), session.responseStoredBytes)
	if session.responseFile != nil {
		require.NoError(t, session.responseFile.Close())
	}
	store.abort(staging)
}

func TestRawExchangeLocalStorageRejectsActiveUnsharedNode(t *testing.T) {
	db, store := setupRawExchangeLocalServiceTest(t)
	previousNodeName := common.NodeName
	common.NodeName = "current-node"
	t.Cleanup(func() { common.NodeName = previousNodeName })
	require.NoError(t, db.AutoMigrate(&model.SystemInstance{}))
	require.NoError(t, db.Create(&model.SystemInstance{
		NodeName:   "other-node",
		StartedAt:  common.GetTimestamp(),
		LastSeenAt: common.GetTimestamp(),
	}).Error)

	store.topologyCheckedAt = time.Time{}
	require.ErrorContains(t, store.ready(), "single node")
	require.NoError(t, os.Setenv("RAW_EXCHANGE_LOCAL_SHARED", "true"))
	store.topologyCheckedAt = time.Time{}
	require.NoError(t, store.ready())
}

func TestRawExchangeReconcileMarksMissingAndDeletesOrphan(t *testing.T) {
	db, store := setupRawExchangeLocalServiceTest(t)
	missing := &model.RawExchangeArchive{
		RequestId:          "missing-object",
		Status:             model.RawExchangeStatusStored,
		StorageBackend:     rawExchangeStorageBackendLocal,
		ObjectKey:          "2026/01/01/missing-object",
		NodeName:           common.NodeName,
		RequestAvailable:   true,
		RequestStoredBytes: 40,
	}
	require.NoError(t, model.CreateRawExchangeArchive(missing, 100))

	orphanKey := "2026/01/01/orphan-object"
	orphanPath, err := store.objectPath(orphanKey)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(orphanPath, 0o700))
	manifestPath := path.Join(orphanPath, rawExchangeManifestFileName)
	require.NoError(t, os.WriteFile(manifestPath, []byte("{}"), 0o600))
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(manifestPath, old, old))

	result := &RawExchangeCleanupResult{}
	reconcileRawExchangeStorage(context.Background(), result)
	assert.Equal(t, 1, result.MissingMarked)
	assert.Equal(t, 1, result.OrphanObjectsDeleted)
	assert.Zero(t, result.ReconcileFailed)

	var actual model.RawExchangeArchive
	require.NoError(t, db.First(&actual, missing.Id).Error)
	assert.Equal(t, model.RawExchangeStatusMissing, actual.Status)
	assert.False(t, actual.RequestAvailable)
	assert.Zero(t, actual.RequestStoredBytes)
	var usage model.RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Zero(t, usage.UsedBytes)
	_, err = os.Stat(orphanPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestRawExchangeCleanupFilterRejectsAmbiguousStatuses(t *testing.T) {
	require.Error(t, ValidateRawExchangeCleanupFilter(model.RawExchangeCleanupFilter{
		Status:   model.RawExchangeStatusStored,
		Statuses: []string{model.RawExchangeStatusStored},
	}))
	require.Error(t, ValidateRawExchangeCleanupFilter(model.RawExchangeCleanupFilter{
		Statuses: []string{model.RawExchangeStatusStored, model.RawExchangeStatusStored},
	}))
}

func TestRawExchangeCleanupPreviewTokenBindsFilterPreviewAndOperator(t *testing.T) {
	setupRawExchangeLocalServiceTest(t)
	previousSecret := common.SessionSecret
	common.SessionSecret = "raw-exchange-preview-test-secret"
	t.Cleanup(func() { common.SessionSecret = previousSecret })
	archive := &model.RawExchangeArchive{
		RequestId:          "preview-token-archive",
		UserId:             7,
		Status:             model.RawExchangeStatusStored,
		StorageBackend:     rawExchangeStorageBackendLocal,
		RequestStoredBytes: 40,
		CreatedAt:          100,
	}
	require.NoError(t, model.CreateRawExchangeArchive(archive, 100))
	filter := model.RawExchangeCleanupFilter{UserId: archive.UserId, BeforeTimestamp: 200}

	preview, token, _, err := CreateRawExchangeCleanupPreview(filter, 11)
	require.NoError(t, err)
	assert.Equal(t, int64(1), preview.Count)
	verifiedFilter, verifiedPreview, err := VerifyRawExchangeCleanupPreview(token, 11)
	require.NoError(t, err)
	assert.Equal(t, filter, verifiedFilter)
	assert.Equal(t, preview, verifiedPreview)
	_, _, err = VerifyRawExchangeCleanupPreview(token, 12)
	require.Error(t, err)
}

func TestRawExchangeCleanupCompletionAuditIncludesResultWithoutPrivateData(t *testing.T) {
	db, _ := setupRawExchangeLocalServiceTest(t)
	previousLogDB := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = previousLogDB })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	operator := &model.User{Username: "cleanup-root", Role: common.RoleRootUser}
	require.NoError(t, db.Create(operator).Error)
	payload := RawExchangeCleanupPayload{
		Filter: model.RawExchangeCleanupFilter{UserId: 7, Status: model.RawExchangeStatusStored},
		Audit: RawExchangeCleanupAuditContext{
			OperatorId:       operator.Id,
			OperatorUsername: operator.Username,
			OperatorRole:     operator.Role,
			AuthMethod:       "session",
			Ip:               "127.0.0.1",
		},
	}
	result := &RawExchangeCleanupResult{
		Matched:              3,
		Processed:            3,
		Deleted:              2,
		Failed:               1,
		RequestBytesFreed:    40,
		ResponseBytesFreed:   20,
		TotalBytesFreed:      60,
		OrphanObjectsDeleted: 1,
	}

	recordRawExchangeCleanupCompletionAudit("cleanup-audit-task", payload, model.SystemTaskStatusSucceeded, result, nil)

	var auditLog model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", operator.Id, model.LogTypeManage).First(&auditLog).Error)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(auditLog.Other, &other))
	op, ok := other["op"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "raw_exchange.cleanup_complete", op["action"])
	params, ok := op["params"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "cleanup-audit-task", params["task_id"])
	assert.Equal(t, string(model.SystemTaskStatusSucceeded), params["status"])
	assert.EqualValues(t, result.Deleted, params["deleted"])
	assert.EqualValues(t, result.Failed, params["failed"])
	assert.EqualValues(t, result.TotalBytesFreed, params["total_bytes"])
	adminInfo, ok := other["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, operator.Id, adminInfo["admin_id"])
	assert.Equal(t, operator.Username, adminInfo["admin_username"])
	assert.Equal(t, "session", adminInfo["auth_method"])
	assert.NotContains(t, auditLog.Other, "object_key")
	assert.NotContains(t, auditLog.Other, "token_name")
	assert.NotContains(t, auditLog.Other, "request.body")
}

func TestRawExchangeCleanupHandlerPersistsCompletionAuditAfterTaskResult(t *testing.T) {
	db, _ := setupRawExchangeLocalServiceTest(t)
	previousLogDB := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = previousLogDB })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	operator := &model.User{Username: "cleanup-handler-root", Role: common.RoleRootUser}
	require.NoError(t, db.Create(operator).Error)
	archive := &model.RawExchangeArchive{
		RequestId: "cleanup-handler-archive",
		UserId:    7,
		Status:    model.RawExchangeStatusCaptureFailed,
	}
	require.NoError(t, model.CreateRawExchangeArchive(archive, 100))
	payload := RawExchangeCleanupPayload{
		Filter:    model.RawExchangeCleanupFilter{Status: model.RawExchangeStatusCaptureFailed},
		BatchSize: 100,
		Audit: RawExchangeCleanupAuditContext{
			OperatorId:       operator.Id,
			OperatorUsername: operator.Username,
			OperatorRole:     operator.Role,
			AuthMethod:       "session",
			Ip:               "127.0.0.1",
		},
	}
	task, err := model.CreateSystemTask(model.SystemTaskTypeRawExchangeCleanup, payload, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "audit-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)

	rawExchangeCleanupHandler{}.Run(context.Background(), claimed, "audit-runner")

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, finished)
	assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
	var auditLog model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", operator.Id, model.LogTypeManage).First(&auditLog).Error)
	assert.Contains(t, auditLog.Other, "raw_exchange.cleanup_complete")
	assert.Contains(t, auditLog.Other, task.TaskID)
}

func TestRawExchangeCleanupPurgesDeletedTombstone(t *testing.T) {
	db, _ := setupRawExchangeLocalServiceTest(t)
	archive := &model.RawExchangeArchive{
		RequestId: "cleanup-tombstone",
		UserId:    7,
		Status:    model.RawExchangeStatusCaptureFailed,
	}
	require.NoError(t, model.CreateRawExchangeArchive(archive, 100))
	_, _, _, err := DeleteRawExchangeForUser(archive.RequestId, archive.UserId)
	require.NoError(t, err)

	result, err := runRawExchangeCleanup(context.Background(), &model.SystemTask{TaskID: "cleanup-test"}, "runner", RawExchangeCleanupPayload{
		Filter:    model.RawExchangeCleanupFilter{Status: model.RawExchangeStatusDeleted},
		BatchSize: 100,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.Deleted)
	assert.Equal(t, 1, result.TombstonesPurged)
	var count int64
	require.NoError(t, db.Model(&model.RawExchangeArchive{}).Where("id = ?", archive.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRawExchangeBatchDeleteAccountsForEverySelection(t *testing.T) {
	setupRawExchangeLocalServiceTest(t)
	archive := &model.RawExchangeArchive{
		RequestId:      "batch-delete",
		UserId:         7,
		Status:         model.RawExchangeStatusCaptureFailed,
		StorageBackend: rawExchangeStorageBackendLocal,
	}
	require.NoError(t, model.CreateRawExchangeArchive(archive, 100))

	result := DeleteRawExchangeBatchForUser([]string{archive.RequestId, archive.RequestId, ""}, archive.UserId)
	assert.Equal(t, 3, result.Selected)
	assert.Equal(t, 1, result.Deleted)
	assert.Equal(t, 2, result.Failed)
	assert.Zero(t, result.AlreadyDeleted)
}

func TestRawExchangeDeleteRejectsOtherOwnerWithoutChangingArchive(t *testing.T) {
	db, _ := setupRawExchangeLocalServiceTest(t)
	archive := &model.RawExchangeArchive{
		RequestId:          "private-delete",
		UserId:             7,
		Status:             model.RawExchangeStatusStored,
		StorageBackend:     rawExchangeStorageBackendLocal,
		ObjectKey:          "2026/01/01/private-delete",
		RequestStoredBytes: 40,
	}
	require.NoError(t, model.CreateRawExchangeArchive(archive, 100))
	_, _, _, err := DeleteRawExchangeForUser(archive.RequestId, 99)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var actual model.RawExchangeArchive
	require.NoError(t, db.First(&actual, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusStored, actual.Status)
	var usage model.RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Equal(t, int64(40), usage.UsedBytes)
}

func TestRawExchangeDownloadMarksMissingObject(t *testing.T) {
	db, _ := setupRawExchangeLocalServiceTest(t)
	archive := &model.RawExchangeArchive{
		RequestId:          "download-missing-object",
		UserId:             7,
		Status:             model.RawExchangeStatusStored,
		StorageBackend:     rawExchangeStorageBackendLocal,
		ObjectKey:          "2026/01/01/download-missing-object",
		NodeName:           common.NodeName,
		RequestAvailable:   true,
		RequestStoredBytes: 40,
	}
	require.NoError(t, model.CreateRawExchangeArchive(archive, 100))
	download, err := AcquireRawExchangeDownload(archive.RequestId, archive.UserId)
	require.NoError(t, err)
	_, _, err = download.OpenRequest()
	require.ErrorIs(t, err, model.ErrRawExchangeNotStored)
	require.NoError(t, download.Close())

	var actual model.RawExchangeArchive
	require.NoError(t, db.First(&actual, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusMissing, actual.Status)
	var usage model.RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Zero(t, usage.UsedBytes)
}

func TestRawExchangeS3DownloadRequiresManifestCompletionMarker(t *testing.T) {
	db, store, client := setupRawExchangeS3ServiceTest(t)
	archive, _ := createPendingRawExchangeS3Archive(t, store, "s3-missing-manifest")
	commitPendingRawExchange(archive)
	delete(client.objects, path.Join(archive.ObjectKey, rawExchangeManifestFileName))

	_, err := AcquireRawExchangeDownload(archive.RequestId, archive.UserId)
	require.ErrorIs(t, err, model.ErrRawExchangeNotStored)
	var missing model.RawExchangeArchive
	require.NoError(t, db.First(&missing, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusMissing, missing.Status)
}

func TestRawExchangeS3RequiresExplicitNodeName(t *testing.T) {
	previousNodeName := common.NodeName
	previousManual := common.NodeNameManuallyConfigured
	common.NodeName = "hostname-fallback"
	common.NodeNameManuallyConfigured = false
	t.Cleanup(func() {
		common.NodeName = previousNodeName
		common.NodeNameManuallyConfigured = previousManual
	})

	_, err := newRawExchangeS3Store(rawExchangeSpool{})
	require.ErrorContains(t, err, "NODE_NAME")
}

func TestRawExchangeS3PendingCommitDownloadAndDelete(t *testing.T) {
	db, store, client := setupRawExchangeS3ServiceTest(t)
	requestBody := []byte(`{"model":"gpt-test"}`)
	responseBody := []byte(`{"error":"upstream failed"}`)
	staging, err := store.begin()
	require.NoError(t, err)
	writeRawExchangeTestPart(t, store, staging, rawExchangeRequestFileName, requestBody)
	writeRawExchangeTestPart(t, store, staging, rawExchangeResponseFileName, responseBody)
	createdAt := time.Unix(1_700_000_000, 0)
	objectKey, err := store.objectKey(staging, createdAt)
	require.NoError(t, err)
	archive := &model.RawExchangeArchive{
		RequestId:           "s3-pending-request",
		UserId:              7,
		TokenId:             8,
		Status:              model.RawExchangeStatusPendingCommit,
		StorageBackend:      rawExchangeStorageBackendS3,
		ObjectKey:           objectKey,
		RequestAvailable:    true,
		ResponseAvailable:   true,
		RequestStoredBytes:  int64(len(requestBody)),
		ResponseStoredBytes: int64(len(responseBody)),
		CreatedAt:           time.Now().Unix(),
	}
	require.NoError(t, writeRawExchangeManifest(store, staging, archive))
	require.NoError(t, model.CreateRawExchangeArchive(archive, 1<<20))

	var usage model.RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Zero(t, usage.UsedBytes)

	commitPendingRawExchange(archive)

	var stored model.RawExchangeArchive
	require.NoError(t, db.First(&stored, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusStored, stored.Status)
	assert.Positive(t, stored.CommittedAt)
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Equal(t, int64(len(requestBody)+len(responseBody)), usage.UsedBytes)
	require.Len(t, client.putOrder, 3)
	assert.Equal(t, path.Join(objectKey, rawExchangeRequestFileName), client.putOrder[0])
	assert.Equal(t, path.Join(objectKey, rawExchangeResponseFileName), client.putOrder[1])
	assert.Equal(t, path.Join(objectKey, rawExchangeManifestFileName), client.putOrder[2])
	assert.Equal(t, []s3types.ServerSideEncryption{
		s3types.ServerSideEncryptionAes256,
		s3types.ServerSideEncryptionAes256,
		s3types.ServerSideEncryptionAes256,
	}, client.putSSE)
	_, err = os.Stat(staging.path)
	assert.ErrorIs(t, err, os.ErrNotExist)

	var manifest rawExchangeManifest
	require.NoError(t, common.Unmarshal(client.objects[path.Join(objectKey, rawExchangeManifestFileName)], &manifest))
	require.NotNil(t, manifest.Archive)
	assert.Equal(t, objectKey, manifest.ObjectKey)
	assert.Equal(t, model.RawExchangeStatusStored, manifest.Archive.Status)
	assert.Equal(t, archive.Id, manifest.Archive.Id)

	download, err := AcquireRawExchangeDownload(archive.RequestId, archive.UserId)
	require.NoError(t, err)
	requestReader, size, err := download.OpenRequest()
	require.NoError(t, err)
	actualRequest, err := io.ReadAll(requestReader)
	require.NoError(t, err)
	require.NoError(t, requestReader.Close())
	assert.Equal(t, int64(len(requestBody)), size)
	assert.Equal(t, requestBody, actualRequest)
	require.NoError(t, download.Close())

	alreadyDeleted, requestBytes, responseBytes, err := DeleteRawExchangeForUser(archive.RequestId, archive.UserId)
	require.NoError(t, err)
	assert.False(t, alreadyDeleted)
	assert.Equal(t, int64(len(requestBody)), requestBytes)
	assert.Equal(t, int64(len(responseBody)), responseBytes)
	assert.Empty(t, client.objects)
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Zero(t, usage.UsedBytes)
}

func TestRawExchangeS3TransientFailuresRemainPendingAndRetry(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "provider rejection", err: errors.New("access denied")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, store, client := setupRawExchangeS3ServiceTest(t)
			archive, staging := createPendingRawExchangeS3Archive(t, store, "s3-transient-"+strings.ReplaceAll(test.name, " ", "-"))
			client.nextPutError = test.err

			commitPendingRawExchange(archive)

			var pending model.RawExchangeArchive
			require.NoError(t, db.First(&pending, archive.Id).Error)
			assert.Equal(t, model.RawExchangeStatusPendingCommit, pending.Status)
			assert.Equal(t, "object_commit_failed", pending.ErrorCode)
			assert.Equal(t, 1, pending.CommitAttempts)
			assert.Greater(t, pending.NextCommitAt, common.GetTimestamp())
			_, err := os.Stat(staging.path)
			require.NoError(t, err)

			require.NoError(t, db.Model(&model.RawExchangeArchive{}).Where("id = ?", pending.Id).Update("next_commit_at", 0).Error)
			pending.NextCommitAt = 0
			commitPendingRawExchange(&pending)
			require.NoError(t, db.First(&pending, archive.Id).Error)
			assert.Equal(t, model.RawExchangeStatusStored, pending.Status)
		})
	}
}

func TestRawExchangeS3IntegrationRecoversFromEndpointOutage(t *testing.T) {
	endpoint := os.Getenv("TEST_RAW_EXCHANGE_S3_ENDPOINT")
	accessKey := os.Getenv("TEST_RAW_EXCHANGE_S3_ACCESS_KEY_ID")
	secretKey := os.Getenv("TEST_RAW_EXCHANGE_S3_SECRET_ACCESS_KEY")
	bucket := os.Getenv("TEST_RAW_EXCHANGE_S3_BUCKET")
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		t.Skip("set TEST_RAW_EXCHANGE_S3_* to run the S3 integration test")
	}
	target, err := url.Parse(endpoint)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	var unavailable atomic.Bool
	proxyServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if unavailable.Load() {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(response, request)
	}))
	t.Cleanup(proxyServer.Close)

	db, _ := setupRawExchangeLocalServiceTest(t)
	spool, err := initRawExchangeSpool(t.TempDir())
	require.NoError(t, err)
	previousNodeName := common.NodeName
	previousManual := common.NodeNameManuallyConfigured
	common.NodeName = "s3-integration-node"
	common.NodeNameManuallyConfigured = true
	t.Cleanup(func() {
		common.NodeName = previousNodeName
		common.NodeNameManuallyConfigured = previousManual
	})
	t.Setenv("RAW_EXCHANGE_S3_BUCKET", bucket)
	t.Setenv("RAW_EXCHANGE_S3_REGION", "us-east-1")
	t.Setenv("RAW_EXCHANGE_S3_ACCESS_KEY_ID", accessKey)
	t.Setenv("RAW_EXCHANGE_S3_SECRET_ACCESS_KEY", secretKey)
	t.Setenv("RAW_EXCHANGE_S3_ENDPOINT", proxyServer.URL)
	t.Setenv("RAW_EXCHANGE_S3_USE_PATH_STYLE", "true")
	t.Setenv("RAW_EXCHANGE_S3_SERVER_SIDE_ENCRYPTION", "AES256")
	store, err := newRawExchangeS3Store(spool)
	require.NoError(t, err)
	rawExchangeStoreState.Lock()
	previousStore := rawExchangeStoreState.store
	previousInitErr := rawExchangeStoreState.initErr
	rawExchangeStoreState.store = store
	rawExchangeStoreState.initErr = nil
	rawExchangeStoreState.Unlock()
	t.Cleanup(func() {
		rawExchangeStoreState.Lock()
		rawExchangeStoreState.store = previousStore
		rawExchangeStoreState.initErr = previousInitErr
		rawExchangeStoreState.Unlock()
	})
	archive, staging := createPendingRawExchangeS3Archive(t, store, "s3-integration-outage")

	unavailable.Store(true)
	commitPendingRawExchange(archive)
	var pending model.RawExchangeArchive
	require.NoError(t, db.First(&pending, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusPendingCommit, pending.Status)
	assert.Equal(t, "object_commit_failed", pending.ErrorCode)
	_, err = os.Stat(staging.path)
	require.NoError(t, err)

	unavailable.Store(false)
	require.NoError(t, db.Model(&model.RawExchangeArchive{}).Where("id = ?", pending.Id).Update("next_commit_at", 0).Error)
	pending.NextCommitAt = 0
	commitPendingRawExchange(&pending)
	require.NoError(t, db.First(&pending, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusStored, pending.Status)
	download, err := AcquireRawExchangeDownload(pending.RequestId, pending.UserId)
	require.NoError(t, err)
	requestBody, _, err := download.OpenRequest()
	require.NoError(t, err)
	storedRequest, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	require.NoError(t, requestBody.Close())
	require.NoError(t, download.Close())
	assert.JSONEq(t, `{"model":"gpt-test"}`, string(storedRequest))

	unavailable.Store(true)
	_, _, _, err = DeleteRawExchangeForUser(pending.RequestId, pending.UserId)
	require.Error(t, err)
	var failedDelete model.RawExchangeArchive
	require.NoError(t, db.First(&failedDelete, pending.Id).Error)
	assert.Equal(t, model.RawExchangeStatusDeleteFailed, failedDelete.Status)
	var usage model.RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Positive(t, usage.UsedBytes)

	unavailable.Store(false)
	alreadyDeleted, _, _, err := DeleteRawExchangeForUser(pending.RequestId, pending.UserId)
	require.NoError(t, err)
	assert.False(t, alreadyDeleted)
	var deleted model.RawExchangeArchive
	require.NoError(t, db.First(&deleted, pending.Id).Error)
	assert.Equal(t, model.RawExchangeStatusDeleted, deleted.Status)
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Zero(t, usage.UsedBytes)

	require.NoError(t, os.Setenv("RAW_EXCHANGE_S3_SECRET_ACCESS_KEY", "invalid-secret-key"))
	_, err = newRawExchangeS3Store(spool)
	require.ErrorContains(t, err, "raw exchange S3 bucket is unavailable")
	require.NoError(t, os.Setenv("RAW_EXCHANGE_S3_SECRET_ACCESS_KEY", secretKey))
}

func TestRawExchangeS3CredentialRejectionFailsReadinessWithoutWritingObjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	spool, err := initRawExchangeSpool(t.TempDir())
	require.NoError(t, err)
	previousNodeName := common.NodeName
	previousManual := common.NodeNameManuallyConfigured
	common.NodeName = "credential-test-node"
	common.NodeNameManuallyConfigured = true
	t.Cleanup(func() {
		common.NodeName = previousNodeName
		common.NodeNameManuallyConfigured = previousManual
	})
	t.Setenv("RAW_EXCHANGE_S3_BUCKET", "private-archives")
	t.Setenv("RAW_EXCHANGE_S3_REGION", "us-east-1")
	t.Setenv("RAW_EXCHANGE_S3_ACCESS_KEY_ID", "invalid-access-key")
	t.Setenv("RAW_EXCHANGE_S3_SECRET_ACCESS_KEY", "invalid-secret-key")
	t.Setenv("RAW_EXCHANGE_S3_ENDPOINT", server.URL)
	t.Setenv("RAW_EXCHANGE_S3_USE_PATH_STYLE", "true")
	t.Setenv("RAW_EXCHANGE_S3_SERVER_SIDE_ENCRYPTION", "AES256")

	_, err = newRawExchangeS3Store(spool)

	require.ErrorContains(t, err, "raw exchange S3 bucket is unavailable")
	entries, readErr := os.ReadDir(spool.spoolDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

func TestRawExchangeCrashRecoveryBoundaries(t *testing.T) {
	t.Run("request and partial response before metadata", func(t *testing.T) {
		_, store, _ := setupRawExchangeS3ServiceTest(t)
		staging, err := store.begin()
		require.NoError(t, err)
		writeRawExchangeTestPart(t, store, staging, rawExchangeRequestFileName, []byte("request"))
		writeRawExchangeTestPart(t, store, staging, rawExchangeResponseFileName, []byte("partial-response"))
		old := time.Now().Add(-2 * time.Hour)
		require.NoError(t, os.Chtimes(staging.path, old, old))

		deleted, err := store.cleanupStaleSpool(time.Now().Add(-time.Hour))

		require.NoError(t, err)
		assert.Equal(t, 1, deleted)
		_, err = os.Stat(staging.path)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("manifest upload before metadata commit", func(t *testing.T) {
		db, store, client := setupRawExchangeS3ServiceTest(t)
		archive, staging := createPendingRawExchangeS3Archive(t, store, "crash-before-metadata-commit")
		require.NoError(t, store.commit(staging, archive.ObjectKey))
		assert.Contains(t, client.objects, path.Join(archive.ObjectKey, rawExchangeManifestFileName))

		commitPendingRawExchange(archive)

		var stored model.RawExchangeArchive
		require.NoError(t, db.First(&stored, archive.Id).Error)
		assert.Equal(t, model.RawExchangeStatusStored, stored.Status)
		_, err := os.Stat(staging.path)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("metadata commit before spool cleanup", func(t *testing.T) {
		db, store, client := setupRawExchangeS3ServiceTest(t)
		archive, staging := createPendingRawExchangeS3Archive(t, store, "crash-before-spool-cleanup")
		require.NoError(t, store.commit(staging, archive.ObjectKey))
		require.NoError(t, model.CompleteRawExchangeCommit(archive.Id, common.GetTimestamp(), 1<<20))
		old := time.Now().Add(-2 * time.Hour)
		require.NoError(t, os.Chtimes(staging.path, old, old))

		deleted, err := store.cleanupStaleSpool(time.Now().Add(-time.Hour))

		require.NoError(t, err)
		assert.Equal(t, 1, deleted)
		assert.Contains(t, client.objects, path.Join(archive.ObjectKey, rawExchangeManifestFileName))
		var stored model.RawExchangeArchive
		require.NoError(t, db.First(&stored, archive.Id).Error)
		assert.Equal(t, model.RawExchangeStatusStored, stored.Status)
	})
}

func TestRawExchangeS3CommitRetriesWithoutLosingPendingSpool(t *testing.T) {
	db, store, client := setupRawExchangeS3ServiceTest(t)
	archive, staging := createPendingRawExchangeS3Archive(t, store, "s3-retry-request")
	client.putErrorAtCall = 3

	commitPendingRawExchange(archive)

	var pending model.RawExchangeArchive
	require.NoError(t, db.First(&pending, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusPendingCommit, pending.Status)
	assert.Equal(t, "object_commit_failed", pending.ErrorCode)
	assert.Equal(t, 1, pending.CommitAttempts)
	assert.Greater(t, pending.NextCommitAt, common.GetTimestamp())
	assert.Len(t, client.objects, 2)
	assert.NotContains(t, client.objects, path.Join(archive.ObjectKey, rawExchangeManifestFileName))
	_, err := os.Stat(staging.path)
	require.NoError(t, err)
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(staging.path, old, old))
	deleted, err := store.cleanupStaleSpool(time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, deleted)
	_, err = os.Stat(staging.path)
	require.NoError(t, err)

	client.putErrorAtCall = 0
	require.NoError(t, db.Model(&model.RawExchangeArchive{}).Where("id = ?", pending.Id).Update("next_commit_at", 0).Error)
	pending.NextCommitAt = 0
	commitPendingRawExchange(&pending)
	require.NoError(t, db.First(&pending, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusStored, pending.Status)
	_, err = os.Stat(staging.path)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestRawExchangePendingS3DeleteWaitsForOwningNode(t *testing.T) {
	db, store, client := setupRawExchangeS3ServiceTest(t)
	previousNodeName := common.NodeName
	common.NodeName = "capture-node"
	t.Cleanup(func() { common.NodeName = previousNodeName })
	archive, staging := createPendingRawExchangeS3Archive(t, store, "s3-pending-delete")
	requestKey := path.Join(archive.ObjectKey, rawExchangeRequestFileName)
	client.objects[requestKey] = []byte("partial upload")
	client.modifiedAt[requestKey] = time.Now()

	common.NodeName = "other-node"
	_, _, _, err := DeleteRawExchangeForUser(archive.RequestId, archive.UserId)
	require.ErrorIs(t, err, ErrRawExchangeDeletionDeferred)
	var pending model.RawExchangeArchive
	require.NoError(t, db.First(&pending, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusDeletePending, pending.Status)
	_, err = os.Stat(staging.path)
	require.NoError(t, err)

	common.NodeName = "capture-node"
	processPendingRawExchangeDeletes()
	var deleted model.RawExchangeArchive
	require.NoError(t, db.First(&deleted, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusDeleted, deleted.Status)
	_, err = os.Stat(staging.path)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Empty(t, client.objects)
}

func TestRawExchangeS3DeleteFailurePreservesCapacityUntilRetry(t *testing.T) {
	db, store, client := setupRawExchangeS3ServiceTest(t)
	archive, _ := createPendingRawExchangeS3Archive(t, store, "s3-delete-retry")
	commitPendingRawExchange(archive)
	client.deleteErrorsRemaining = 1

	_, _, _, err := DeleteRawExchangeForUser(archive.RequestId, archive.UserId)
	require.Error(t, err)
	var failed model.RawExchangeArchive
	require.NoError(t, db.First(&failed, archive.Id).Error)
	assert.Equal(t, model.RawExchangeStatusDeleteFailed, failed.Status)
	var usage model.RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Positive(t, usage.UsedBytes)

	alreadyDeleted, _, _, err := DeleteRawExchangeForUser(archive.RequestId, archive.UserId)
	require.NoError(t, err)
	assert.False(t, alreadyDeleted)
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Zero(t, usage.UsedBytes)
	assert.Empty(t, client.objects)
}

func TestRawExchangeS3CommitRejectsEscapingObjectKey(t *testing.T) {
	_, store, client := setupRawExchangeS3ServiceTest(t)
	staging, err := store.begin()
	require.NoError(t, err)
	writeRawExchangeTestPart(t, store, staging, rawExchangeRequestFileName, []byte("request"))
	writeRawExchangeTestPart(t, store, staging, rawExchangeManifestFileName, []byte("{}"))

	require.Error(t, store.commit(staging, "../outside"))
	assert.Empty(t, client.objects)
	store.abort(staging)
}
