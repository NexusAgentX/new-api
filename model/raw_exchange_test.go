package model

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupRawExchangeModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousMainType := common.MainDatabaseType()
	previousLogType := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&Token{}, &RawExchangeArchive{}, &RawExchangeStorageUsage{}, &RawExchangeDownloadLease{}))
	require.NoError(t, InitializeRawExchangeStorageUsage())

	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestRawExchangeDownloadMetadataIncludesIntegrityWithoutPrivateStorageIdentity(t *testing.T) {
	archive := &RawExchangeArchive{
		RequestId:              "metadata-request",
		UserId:                 7,
		TokenId:                8,
		ObjectKey:              "private/object/key",
		NodeName:               "private-node",
		RequestMethod:          "POST",
		RequestPath:            "/v1/responses",
		RequestContentType:     "application/json",
		RequestContentEncoding: "gzip",
		RequestSha256:          strings.Repeat("a", 64),
	}
	data, err := common.Marshal(archive.DownloadMetadata())
	require.NoError(t, err)
	var metadata map[string]any
	require.NoError(t, common.Unmarshal(data, &metadata))
	assert.Equal(t, archive.RequestId, metadata["request_id"])
	assert.Equal(t, archive.RequestMethod, metadata["request_method"])
	assert.Equal(t, archive.RequestPath, metadata["request_path"])
	assert.Equal(t, archive.RequestContentEncoding, metadata["request_original_content_encoding"])
	assert.Equal(t, archive.RequestSha256, metadata["request_sha256"])
	assert.NotContains(t, metadata, "user_id")
	assert.NotContains(t, metadata, "token_id")
	assert.NotContains(t, metadata, "object_key")
	assert.NotContains(t, metadata, "node_name")
}

func TestNormalizeRawExchangeCaptureMode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{name: "empty defaults off", input: "", expected: RawExchangeCaptureOff},
		{name: "trims and normalizes", input: " ALL ", expected: RawExchangeCaptureAll},
		{name: "non success", input: RawExchangeCaptureNonSuccess, expected: RawExchangeCaptureNonSuccess},
		{name: "invalid", input: "failed", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := NormalizeRawExchangeCaptureMode(test.input)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
	assert.Equal(t, RawExchangeCaptureOff, EffectiveRawExchangeCaptureMode("invalid"))
}

func TestBackfillRawExchangeCaptureModesPreservesExplicitModes(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	tokens := []Token{
		{UserId: 1, Key: "empty", RawExchangeCaptureMode: ""},
		{UserId: 1, Key: "all", RawExchangeCaptureMode: RawExchangeCaptureAll},
	}
	require.NoError(t, db.Create(&tokens).Error)

	require.NoError(t, BackfillRawExchangeCaptureModes())

	var actual []Token
	require.NoError(t, db.Order("id").Find(&actual).Error)
	require.Len(t, actual, 2)
	assert.Equal(t, RawExchangeCaptureOff, actual[0].RawExchangeCaptureMode)
	assert.Equal(t, RawExchangeCaptureAll, actual[1].RawExchangeCaptureMode)
}

func TestCreateRawExchangeArchiveEnforcesCapacityAtomically(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	first := &RawExchangeArchive{
		RequestId:           "request-1",
		Status:              RawExchangeStatusStored,
		RequestStoredBytes:  40,
		ResponseStoredBytes: 20,
	}
	require.NoError(t, CreateRawExchangeArchive(first, 100))

	second := &RawExchangeArchive{
		RequestId:           "request-2",
		Status:              RawExchangeStatusStored,
		RequestStoredBytes:  30,
		ResponseStoredBytes: 20,
	}
	require.ErrorIs(t, CreateRawExchangeArchive(second, 100), ErrRawExchangeCapacityExceeded)

	var usage RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Equal(t, int64(60), usage.UsedBytes)
	var count int64
	require.NoError(t, db.Model(&RawExchangeArchive{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestPendingRawExchangeCommitUsesPersistedRetryBackoff(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	archive := &RawExchangeArchive{
		RequestId:      "pending-retry",
		Status:         RawExchangeStatusPendingCommit,
		StorageBackend: "s3",
		NodeName:       "retry-node",
	}
	require.NoError(t, CreateRawExchangeArchive(archive, 100))
	require.NoError(t, UpdateRawExchangePendingCommitError(archive.Id, "upload_failed"))

	var pending RawExchangeArchive
	require.NoError(t, db.First(&pending, archive.Id).Error)
	assert.Equal(t, 1, pending.CommitAttempts)
	assert.Greater(t, pending.NextCommitAt, common.GetTimestamp())
	candidates, err := ListPendingRawExchangeCommits(archive.NodeName, 100)
	require.NoError(t, err)
	assert.Empty(t, candidates)

	require.NoError(t, db.Model(&RawExchangeArchive{}).Where("id = ?", archive.Id).Update("next_commit_at", 0).Error)
	candidates, err = ListPendingRawExchangeCommits(archive.NodeName, 100)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, archive.Id, candidates[0].Id)
}

func TestCompleteRawExchangePendingCommitAccountsCapacity(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	first := &RawExchangeArchive{
		RequestId:           "pending-1",
		Status:              RawExchangeStatusPendingCommit,
		StorageBackend:      "s3",
		ObjectKey:           "raw-exchanges/2026/01/01/pending-1",
		RequestStoredBytes:  40,
		ResponseStoredBytes: 20,
	}
	require.NoError(t, CreateRawExchangeArchive(first, 100))

	var usage RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Zero(t, usage.UsedBytes)
	require.NoError(t, CompleteRawExchangeCommit(first.Id, 1234, 100))
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Equal(t, int64(60), usage.UsedBytes)

	var stored RawExchangeArchive
	require.NoError(t, db.First(&stored, first.Id).Error)
	assert.Equal(t, RawExchangeStatusStored, stored.Status)
	assert.Equal(t, int64(1234), stored.CommittedAt)

	second := &RawExchangeArchive{
		RequestId:           "pending-2",
		Status:              RawExchangeStatusPendingCommit,
		StorageBackend:      "s3",
		ObjectKey:           "raw-exchanges/2026/01/01/pending-2",
		RequestStoredBytes:  30,
		ResponseStoredBytes: 20,
	}
	require.NoError(t, CreateRawExchangeArchive(second, 100))
	require.ErrorIs(t, CompleteRawExchangeCommit(second.Id, 1235, 100), ErrRawExchangeCapacityExceeded)
	require.NoError(t, FailRawExchangePendingCommit(second.Id, RawExchangeStatusSkippedCapacity, "capacity_exceeded"))
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Equal(t, int64(60), usage.UsedBytes)

	var skipped RawExchangeArchive
	require.NoError(t, db.First(&skipped, second.Id).Error)
	assert.Equal(t, RawExchangeStatusSkippedCapacity, skipped.Status)
	assert.Empty(t, skipped.ObjectKey)
	assert.Zero(t, skipped.RequestStoredBytes)
	assert.Zero(t, skipped.ResponseStoredBytes)
}

func TestDeletingPendingS3ArchiveDoesNotReleaseCommittedCapacity(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	stored := &RawExchangeArchive{
		RequestId:          "committed-local",
		Status:             RawExchangeStatusStored,
		StorageBackend:     "local",
		RequestStoredBytes: 60,
	}
	require.NoError(t, CreateRawExchangeArchive(stored, 100))
	pending := &RawExchangeArchive{
		RequestId:          "pending-s3-delete",
		UserId:             7,
		Status:             RawExchangeStatusPendingCommit,
		StorageBackend:     "s3",
		ObjectKey:          "raw-exchanges/2026/01/01/pending-delete",
		RequestStoredBytes: 30,
	}
	require.NoError(t, CreateRawExchangeArchive(pending, 100))

	prepared, alreadyDeleted, err := BeginRawExchangeDelete(pending.RequestId, pending.UserId)
	require.NoError(t, err)
	assert.False(t, alreadyDeleted)
	require.NoError(t, CompleteRawExchangeDelete(prepared.Id))

	var usage RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Equal(t, int64(60), usage.UsedBytes)
}

func TestPreviewRawExchangeCleanupSupportsTimeRange(t *testing.T) {
	setupRawExchangeModelTestDB(t)
	for index, createdAt := range []int64{100, 200, 300} {
		require.NoError(t, CreateRawExchangeArchive(&RawExchangeArchive{
			RequestId: fmt.Sprintf("range-%d", index),
			Status:    RawExchangeStatusCaptureFailed,
			CreatedAt: createdAt,
		}, 100))
	}

	preview, err := PreviewRawExchangeCleanup(RawExchangeCleanupFilter{
		AfterTimestamp:  150,
		BeforeTimestamp: 300,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), preview.Count)
}

func TestRawExchangeTombstoneExpiresAndCanBePurged(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	archive := &RawExchangeArchive{
		RequestId: "expired-tombstone",
		UserId:    7,
		Status:    RawExchangeStatusCaptureFailed,
		ExpiresAt: 100,
	}
	require.NoError(t, CreateRawExchangeArchive(archive, 100))
	prepared, alreadyDeleted, err := BeginRawExchangeDelete(archive.RequestId, archive.UserId)
	require.NoError(t, err)
	assert.False(t, alreadyDeleted)
	require.NoError(t, CompleteRawExchangeDelete(prepared.Id))

	var deleted RawExchangeArchive
	require.NoError(t, db.First(&deleted, archive.Id).Error)
	preview, err := PreviewRawExchangeCleanup(RawExchangeCleanupFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), preview.Count)
	candidates, err := ListExpiredRawExchangeTombstones(100, 0, 100)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	purged, err := PurgeRawExchangeTombstoneById(archive.Id)
	require.NoError(t, err)
	assert.True(t, purged)
	require.ErrorIs(t, db.First(&deleted, archive.Id).Error, gorm.ErrRecordNotFound)
}

func TestRawExchangeGCSelectsOnlyExpiredPendingCommits(t *testing.T) {
	setupRawExchangeModelTestDB(t)
	oldPending := &RawExchangeArchive{
		RequestId: "old-pending",
		Status:    RawExchangeStatusPendingCommit,
		CreatedAt: 100,
	}
	freshPending := &RawExchangeArchive{
		RequestId: "fresh-pending",
		Status:    RawExchangeStatusPendingCommit,
		CreatedAt: 200,
	}
	require.NoError(t, CreateRawExchangeArchive(oldPending, 100))
	require.NoError(t, CreateRawExchangeArchive(freshPending, 100))

	candidates, err := ListRawExchangeGCCandidates(1_000, 150, 0, 100)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, oldPending.Id, candidates[0].Id)
}

func TestReconcileRawExchangeStorageUsageRepairsLedger(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	committed := &RawExchangeArchive{
		RequestId:          "usage-committed",
		Status:             RawExchangeStatusStored,
		StorageBackend:     "local",
		RequestStoredBytes: 60,
	}
	pending := &RawExchangeArchive{
		RequestId:          "usage-pending",
		Status:             RawExchangeStatusPendingCommit,
		StorageBackend:     "s3",
		RequestStoredBytes: 50,
	}
	require.NoError(t, CreateRawExchangeArchive(committed, 100))
	require.NoError(t, CreateRawExchangeArchive(pending, 100))
	require.NoError(t, db.Model(&RawExchangeStorageUsage{}).Where("id = ?", 1).Update("used_bytes", 5).Error)

	adjustment, err := ReconcileRawExchangeStorageUsage()
	require.NoError(t, err)
	assert.Equal(t, int64(55), adjustment)
	var usage RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Equal(t, int64(60), usage.UsedBytes)
}

func TestRawExchangeAdminStatsUsesEmptyArrays(t *testing.T) {
	setupRawExchangeModelTestDB(t)

	stats, err := GetRawExchangeAdminStats(10)
	require.NoError(t, err)
	assert.NotNil(t, stats.Statuses)
	assert.NotNil(t, stats.TopUsers)
	assert.NotNil(t, stats.TopTokens)
	assert.Empty(t, stats.Statuses)
	assert.Empty(t, stats.TopUsers)
	assert.Empty(t, stats.TopTokens)
}

func TestRawExchangeAdminStatsAggregatesMetadata(t *testing.T) {
	setupRawExchangeModelTestDB(t)
	stored := &RawExchangeArchive{
		RequestId:           "stats-stored",
		UserId:              7,
		TokenId:             8,
		Status:              RawExchangeStatusStored,
		RequestBytes:        100,
		ResponseBytes:       200,
		RequestStoredBytes:  40,
		ResponseStoredBytes: 20,
		CreatedAt:           100,
	}
	rejected := &RawExchangeArchive{
		RequestId:     "stats-failed",
		UserId:        9,
		TokenId:       10,
		Status:        RawExchangeStatusCaptureFailed,
		RequestBytes:  25,
		ResponseBytes: 50,
		CreatedAt:     200,
	}
	require.NoError(t, CreateRawExchangeArchive(stored, 100))
	require.NoError(t, CreateRawExchangeArchive(rejected, 100))

	stats, err := GetRawExchangeAdminStats(10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.TotalCount)
	assert.Equal(t, int64(125), stats.RequestBytes)
	assert.Equal(t, int64(250), stats.ResponseBytes)
	assert.Equal(t, int64(60), stats.UsedBytes)
	assert.Equal(t, int64(100), stats.OldestCreatedAt)
	assert.Equal(t, int64(200), stats.NewestCreatedAt)
	require.Len(t, stats.TopUsers, 1)
	assert.Equal(t, 7, stats.TopUsers[0].ScopeId)
	assert.Equal(t, int64(60), stats.TopUsers[0].StoredBytes)
	require.Len(t, stats.TopTokens, 1)
	assert.Equal(t, 8, stats.TopTokens[0].ScopeId)
}

func TestRawExchangeDownloadLeaseBlocksDeletionAndPreservesCapacity(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	archive := &RawExchangeArchive{
		RequestId:           "leased-request",
		UserId:              7,
		TokenId:             8,
		Status:              RawExchangeStatusStored,
		StorageBackend:      "local",
		ObjectKey:           "2026/01/01/object",
		RequestAvailable:    true,
		RequestStoredBytes:  40,
		ResponseStoredBytes: 20,
	}
	require.NoError(t, CreateRawExchangeArchive(archive, 100))

	_, leaseId, err := AcquireRawExchangeDownload(archive.RequestId, archive.UserId, 300)
	require.NoError(t, err)
	_, _, err = BeginRawExchangeDelete(archive.RequestId, archive.UserId)
	require.ErrorIs(t, err, ErrRawExchangeDownloadInProgress)

	require.NoError(t, ReleaseRawExchangeDownload(leaseId))
	pending, alreadyDeleted, err := BeginRawExchangeDelete(archive.RequestId, archive.UserId)
	require.NoError(t, err)
	assert.False(t, alreadyDeleted)
	_, _, err = PrepareRawExchangeDeleteById(archive.Id)
	require.ErrorIs(t, err, ErrRawExchangeDeletionInProgress)
	require.NoError(t, CompleteRawExchangeDelete(pending.Id))

	var usage RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Zero(t, usage.UsedBytes)
	var deleted RawExchangeArchive
	require.NoError(t, db.First(&deleted, archive.Id).Error)
	assert.Equal(t, RawExchangeStatusDeleted, deleted.Status)
	assert.False(t, deleted.RequestAvailable)
	assert.Zero(t, deleted.RequestStoredBytes)
	assert.Zero(t, deleted.ResponseStoredBytes)
	assert.Empty(t, deleted.ObjectKey)

	stats, err := GetRawExchangeAdminStats(10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.TotalCount)
	require.Len(t, stats.Statuses, 1)
	assert.Equal(t, RawExchangeStatusDeleted, stats.Statuses[0].Status)
	assert.Zero(t, stats.Statuses[0].StoredBytes)

	_, alreadyDeleted, err = BeginRawExchangeDelete(archive.RequestId, archive.UserId)
	require.NoError(t, err)
	assert.True(t, alreadyDeleted)
}

func TestRawExchangeDownloadRejectsOtherOwner(t *testing.T) {
	setupRawExchangeModelTestDB(t)
	archive := &RawExchangeArchive{
		RequestId:      "private-request",
		UserId:         7,
		Status:         RawExchangeStatusStored,
		StorageBackend: "local",
		ObjectKey:      "2026/01/01/object",
	}
	require.NoError(t, CreateRawExchangeArchive(archive, 100))

	_, _, err := AcquireRawExchangeDownload(archive.RequestId, 99, 300)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestTokenDeleteMarksRawExchangesForGC(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	token := &Token{UserId: 7, Key: "delete-token", RawExchangeCaptureMode: RawExchangeCaptureAll}
	require.NoError(t, db.Create(token).Error)
	archive := &RawExchangeArchive{
		RequestId:      "token-request",
		UserId:         token.UserId,
		TokenId:        token.Id,
		Status:         RawExchangeStatusStored,
		StorageBackend: "local",
		ObjectKey:      "2026/01/01/object",
	}
	require.NoError(t, CreateRawExchangeArchive(archive, 100))

	require.NoError(t, token.Delete())
	var actual RawExchangeArchive
	require.NoError(t, db.First(&actual, archive.Id).Error)
	assert.Equal(t, RawExchangeStatusDeletePending, actual.Status)
	assert.Equal(t, "token_deleted", actual.ErrorCode)
	prepared, alreadyDeleted, err := PrepareRawExchangeDeleteById(actual.Id)
	require.NoError(t, err)
	assert.False(t, alreadyDeleted)
	assert.Equal(t, actual.Id, prepared.Id)
}

func TestAttachRawExchangeSummariesRespectsOwner(t *testing.T) {
	setupRawExchangeModelTestDB(t)
	archive := &RawExchangeArchive{
		RequestId: "summary-request",
		UserId:    7,
		Status:    RawExchangeStatusCaptureFailed,
		Outcome:   RawExchangeOutcomeNonSuccess,
	}
	require.NoError(t, CreateRawExchangeArchive(archive, 100))

	ownerLogs := []*Log{{RequestId: archive.RequestId}}
	ownerId := 7
	require.NoError(t, AttachRawExchangeSummaries(ownerLogs, &ownerId))
	require.NotNil(t, ownerLogs[0].RawExchange)
	assert.Equal(t, RawExchangeStatusCaptureFailed, ownerLogs[0].RawExchange.Status)

	otherLogs := []*Log{{RequestId: archive.RequestId}}
	otherId := 8
	require.NoError(t, AttachRawExchangeSummaries(otherLogs, &otherId))
	assert.Nil(t, otherLogs[0].RawExchange)
}

func TestTokenUpdateRefreshesRawExchangeCaptureModeInRedis(t *testing.T) {
	setupRawExchangeModelTestDB(t)
	server := miniredis.RunT(t)
	addresses := []struct {
		name string
		addr string
	}{{name: "miniredis", addr: server.Addr()}}
	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		addresses = append(addresses, struct {
			name string
			addr string
		}{name: "redis", addr: addr})
	}
	for _, address := range addresses {
		t.Run(address.name, func(t *testing.T) {
			previousRedisEnabled := common.RedisEnabled
			previousRDB := common.RDB
			previousSyncFrequency := common.SyncFrequency
			common.RedisEnabled = true
			common.SyncFrequency = 2
			common.RDB = redis.NewClient(&redis.Options{Addr: address.addr})
			t.Cleanup(func() {
				_ = common.RDB.Close()
				common.RedisEnabled = previousRedisEnabled
				common.RDB = previousRDB
				common.SyncFrequency = previousSyncFrequency
			})
			require.NoError(t, common.RDB.FlushDB(context.Background()).Err())

			token := &Token{
				UserId:                 7,
				Key:                    "raw-exchange-cache-token-" + address.name,
				Name:                   "raw-exchange-cache-token-" + address.name,
				Status:                 common.TokenStatusEnabled,
				RawExchangeCaptureMode: RawExchangeCaptureOff,
			}
			require.NoError(t, token.Insert())
			require.NoError(t, cacheSetToken(*token))
			cached, err := GetTokenByKey(token.Key, false)
			require.NoError(t, err)
			assert.Equal(t, RawExchangeCaptureOff, cached.RawExchangeCaptureMode)

			for _, mode := range []string{RawExchangeCaptureNonSuccess, RawExchangeCaptureAll, RawExchangeCaptureOff} {
				token.RawExchangeCaptureMode = mode
				require.NoError(t, token.Update())
				cached, err = GetTokenByKey(token.Key, false)
				require.NoError(t, err)
				assert.Equal(t, mode, cached.RawExchangeCaptureMode)
			}
		})
	}
}

func TestRawExchangeLifecycleAcrossSQLDialects(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		dbType    common.DatabaseType
		dialector func(string) gorm.Dialector
	}{
		{name: "sqlite", dbType: common.DatabaseTypeSQLite, dialector: func(string) gorm.Dialector {
			return sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_")))
		}},
		{name: "mysql", env: "TEST_MYSQL_DSN", dbType: common.DatabaseTypeMySQL, dialector: func(dsn string) gorm.Dialector { return mysql.Open(dsn) }},
		{name: "postgres", env: "TEST_POSTGRES_DSN", dbType: common.DatabaseTypePostgreSQL, dialector: func(dsn string) gorm.Dialector { return postgres.Open(dsn) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := ""
			if test.env != "" {
				dsn = os.Getenv(test.env)
				if dsn == "" {
					t.Skipf("set %s to run the %s raw exchange lifecycle test", test.env, test.name)
				}
			}
			previousDB := DB
			previousMainType := common.MainDatabaseType()
			previousLogType := common.LogDatabaseType()
			db, err := gorm.Open(test.dialector(dsn), &gorm.Config{})
			require.NoError(t, err)
			DB = db
			common.SetDatabaseTypes(test.dbType, common.DatabaseTypeSQLite)
			t.Cleanup(func() {
				DB = previousDB
				common.SetDatabaseTypes(previousMainType, previousLogType)
				sqlDB, dbErr := db.DB()
				if dbErr == nil {
					_ = sqlDB.Close()
				}
			})

			require.NoError(t, db.Migrator().DropTable(&RawExchangeDownloadLease{}, &RawExchangeArchive{}, &RawExchangeStorageUsage{}))
			require.NoError(t, db.AutoMigrate(&RawExchangeArchive{}, &RawExchangeStorageUsage{}, &RawExchangeDownloadLease{}))
			require.NoError(t, InitializeRawExchangeStorageUsage())
			archive := &RawExchangeArchive{
				RequestId:           "dialect-pending",
				UserId:              7,
				Status:              RawExchangeStatusPendingCommit,
				StorageBackend:      "s3",
				ObjectKey:           "raw-exchanges/2026/01/01/dialect-pending",
				RequestAvailable:    true,
				RequestStoredBytes:  40,
				ResponseStoredBytes: 20,
			}
			require.NoError(t, CreateRawExchangeArchive(archive, 100))
			require.NoError(t, CompleteRawExchangeCommit(archive.Id, 1234, 100))
			require.NoError(t, db.Model(&RawExchangeStorageUsage{}).Where("id = ?", 1).Update("used_bytes", 5).Error)
			adjustment, err := ReconcileRawExchangeStorageUsage()
			require.NoError(t, err)
			assert.Equal(t, int64(55), adjustment)

			downloaded, leaseId, err := AcquireRawExchangeDownload(archive.RequestId, archive.UserId, 300)
			require.NoError(t, err)
			assert.Equal(t, archive.Id, downloaded.Id)
			require.NoError(t, ReleaseRawExchangeDownload(leaseId))
			prepared, alreadyDeleted, err := BeginRawExchangeDelete(archive.RequestId, archive.UserId)
			require.NoError(t, err)
			assert.False(t, alreadyDeleted)
			require.NoError(t, CompleteRawExchangeDelete(prepared.Id))

			var usage RawExchangeStorageUsage
			require.NoError(t, db.First(&usage, 1).Error)
			assert.Zero(t, usage.UsedBytes)

			concurrent := &RawExchangeArchive{
				RequestId:          "dialect-concurrent-commit-delete",
				UserId:             7,
				Status:             RawExchangeStatusPendingCommit,
				StorageBackend:     "s3",
				ObjectKey:          "raw-exchanges/2026/01/01/dialect-concurrent",
				RequestStoredBytes: 40,
			}
			require.NoError(t, CreateRawExchangeArchive(concurrent, 100))
			start := make(chan struct{})
			errorsByOperation := make(chan error, 2)
			var waitGroup sync.WaitGroup
			waitGroup.Add(2)
			go func() {
				defer waitGroup.Done()
				<-start
				errorsByOperation <- CompleteRawExchangeCommit(concurrent.Id, 2000, 100)
			}()
			go func() {
				defer waitGroup.Done()
				<-start
				prepared, alreadyDeleted, deleteErr := BeginRawExchangeDelete(concurrent.RequestId, concurrent.UserId)
				if deleteErr == nil && !alreadyDeleted {
					deleteErr = CompleteRawExchangeDelete(prepared.Id)
				}
				errorsByOperation <- deleteErr
			}()
			close(start)
			waitGroup.Wait()
			close(errorsByOperation)
			for operationErr := range errorsByOperation {
				if operationErr == nil {
					continue
				}
				if test.dbType == common.DatabaseTypeSQLite && strings.Contains(operationErr.Error(), "locked") {
					continue
				}
				assert.ErrorIs(t, operationErr, ErrRawExchangeCommitStateChanged)
			}

			var concurrentState RawExchangeArchive
			require.NoError(t, db.First(&concurrentState, concurrent.Id).Error)
			switch concurrentState.Status {
			case RawExchangeStatusPendingCommit:
				require.NoError(t, CompleteRawExchangeCommit(concurrentState.Id, 2001, 100))
				prepared, alreadyDeleted, deleteErr := BeginRawExchangeDelete(concurrentState.RequestId, concurrentState.UserId)
				require.NoError(t, deleteErr)
				if !alreadyDeleted {
					require.NoError(t, CompleteRawExchangeDelete(prepared.Id))
				}
			case RawExchangeStatusStored:
				prepared, alreadyDeleted, deleteErr := BeginRawExchangeDelete(concurrentState.RequestId, concurrentState.UserId)
				require.NoError(t, deleteErr)
				if !alreadyDeleted {
					require.NoError(t, CompleteRawExchangeDelete(prepared.Id))
				}
			case RawExchangeStatusDeletePending:
				require.NoError(t, CompleteRawExchangeDelete(concurrentState.Id))
			case RawExchangeStatusDeleted:
			default:
				t.Fatalf("unexpected concurrent archive status %q", concurrentState.Status)
			}
			require.NoError(t, db.First(&usage, 1).Error)
			assert.Zero(t, usage.UsedBytes)
			require.NoError(t, db.First(&concurrentState, concurrent.Id).Error)
			assert.Equal(t, RawExchangeStatusDeleted, concurrentState.Status)
		})
	}
}

func TestCreateSkippedRawExchangeDoesNotConsumeCapacity(t *testing.T) {
	db := setupRawExchangeModelTestDB(t)
	archive := &RawExchangeArchive{
		RequestId:           "request-skipped",
		Status:              RawExchangeStatusSkippedTooLarge,
		RequestStoredBytes:  100,
		ResponseStoredBytes: 100,
	}
	require.NoError(t, CreateRawExchangeArchive(archive, 1))

	var usage RawExchangeStorageUsage
	require.NoError(t, db.First(&usage, 1).Error)
	assert.Zero(t, usage.UsedBytes)
}
