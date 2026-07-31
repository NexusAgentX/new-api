package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/google/uuid"
)

const (
	rawExchangeStorageBackendLocal = "local"
	rawExchangeStorageBackendS3    = "s3"
	rawExchangeRequestFileName     = "request.bin"
	rawExchangeResponseFileName    = "response.bin"
	rawExchangeManifestFileName    = "manifest.json"
)

type rawExchangeStore interface {
	backend() string
	ready() error
	begin() (*rawExchangeStaging, error)
	createPart(staging *rawExchangeStaging, name string) (*os.File, error)
	objectKey(staging *rawExchangeStaging, createdAt time.Time) (string, error)
	commit(staging *rawExchangeStaging, objectKey string) error
	abort(staging *rawExchangeStaging)
	open(objectKey, part string) (io.ReadCloser, int64, error)
	delete(objectKey string) error
	partExists(objectKey, part string) (bool, error)
	scanCommittedObjects(cursor string, limit int) ([]rawExchangeCommittedObject, string, bool, error)
	stagingForObjectKey(objectKey string) (*rawExchangeStaging, error)
	cleanupStaleSpool(before time.Time) (int, error)
}

type rawExchangeSpool struct {
	spoolDir string
}

type rawExchangeLocalStore struct {
	spool      rawExchangeSpool
	objectsDir string

	topologyMu        sync.Mutex
	topologyCheckedAt time.Time
	topologyErr       error
}

type rawExchangeS3API interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	GetBucketEncryption(context.Context, *s3.GetBucketEncryptionInput, ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

type rawExchangeS3Store struct {
	spool                rawExchangeSpool
	client               rawExchangeS3API
	bucket               string
	prefix               string
	serverSideEncryption s3types.ServerSideEncryption
	kmsKeyId             string
}

type rawExchangeCommittedObject struct {
	objectKey  string
	modifiedAt time.Time
}

type rawExchangeStaging struct {
	id   string
	path string
}

var rawExchangeStoreState struct {
	sync.RWMutex
	store   rawExchangeStore
	initErr error
}

func InitRawExchangeStorage() error {
	storagePath := strings.TrimSpace(os.Getenv("RAW_EXCHANGE_STORAGE_PATH"))
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("RAW_EXCHANGE_STORAGE_BACKEND")))
	if backend == "" {
		backend = rawExchangeStorageBackendLocal
	}

	rawExchangeStoreState.Lock()
	defer rawExchangeStoreState.Unlock()
	rawExchangeStoreState.store = nil
	rawExchangeStoreState.initErr = nil

	if storagePath == "" {
		return nil
	}
	absolutePath, err := filepath.Abs(storagePath)
	if err != nil {
		rawExchangeStoreState.initErr = err
		return err
	}
	spool, err := initRawExchangeSpool(absolutePath)
	if err != nil {
		rawExchangeStoreState.initErr = err
		return err
	}

	var store rawExchangeStore
	switch backend {
	case rawExchangeStorageBackendLocal:
		if err := validateRawExchangeLocalTopology(); err != nil {
			rawExchangeStoreState.initErr = err
			return err
		}
		objectsDir := filepath.Join(absolutePath, "objects")
		if err := ensurePrivateRawExchangeDir(objectsDir); err != nil {
			rawExchangeStoreState.initErr = err
			return err
		}
		store = &rawExchangeLocalStore{spool: spool, objectsDir: objectsDir}
	case rawExchangeStorageBackendS3:
		store, err = newRawExchangeS3Store(spool)
		if err != nil {
			rawExchangeStoreState.initErr = err
			return err
		}
	default:
		err = fmt.Errorf("unsupported raw exchange storage backend %q", backend)
		rawExchangeStoreState.initErr = err
		return err
	}

	rawExchangeStoreState.store = store
	return nil
}

func RawExchangeStorageBackend() string {
	store, err := rawExchangeStoreSnapshot()
	if err == nil {
		return store.backend()
	}
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("RAW_EXCHANGE_STORAGE_BACKEND")))
	if backend == "" {
		return rawExchangeStorageBackendLocal
	}
	return backend
}

func rawExchangeStoreSnapshot() (rawExchangeStore, error) {
	rawExchangeStoreState.RLock()
	defer rawExchangeStoreState.RUnlock()
	if rawExchangeStoreState.store != nil {
		return rawExchangeStoreState.store, nil
	}
	if rawExchangeStoreState.initErr != nil {
		return nil, rawExchangeStoreState.initErr
	}
	return nil, errors.New("raw exchange storage path is not configured")
}

func initRawExchangeSpool(rootDir string) (rawExchangeSpool, error) {
	if err := ensurePrivateRawExchangeDir(rootDir); err != nil {
		return rawExchangeSpool{}, err
	}
	spoolDir := filepath.Join(rootDir, "spool")
	if err := ensurePrivateRawExchangeDir(spoolDir); err != nil {
		return rawExchangeSpool{}, err
	}
	return rawExchangeSpool{spoolDir: spoolDir}, nil
}

func ensurePrivateRawExchangeDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

func rawExchangeLocalStorageShared() (bool, error) {
	configured := strings.TrimSpace(os.Getenv("RAW_EXCHANGE_LOCAL_SHARED"))
	if configured == "" {
		return false, nil
	}
	shared, err := strconv.ParseBool(configured)
	if err != nil {
		return false, fmt.Errorf("invalid RAW_EXCHANGE_LOCAL_SHARED: %w", err)
	}
	return shared, nil
}

func validateRawExchangeLocalTopology() error {
	shared, err := rawExchangeLocalStorageShared()
	if err != nil {
		return err
	}
	if shared {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("NODE_TYPE")), "slave") {
		return errors.New("local raw exchange storage is unavailable on a slave node unless RAW_EXCHANGE_LOCAL_SHARED=true")
	}
	if model.DB == nil || !model.DB.Migrator().HasTable(&model.SystemInstance{}) {
		return nil
	}
	var otherActiveNodes int64
	now := common.GetTimestamp()
	err = model.DB.Model(&model.SystemInstance{}).
		Where("last_seen_at >= ? AND node_name <> ?", now-model.SystemInstanceStaleAfterSeconds, common.NodeName).
		Count(&otherActiveNodes).Error
	if err != nil {
		return fmt.Errorf("check raw exchange storage topology: %w", err)
	}
	if otherActiveNodes > 0 {
		return errors.New("local raw exchange storage requires a single node or RAW_EXCHANGE_LOCAL_SHARED=true; use the S3 backend for ordinary multi-node deployments")
	}
	return nil
}

func (s rawExchangeSpool) begin() (*rawExchangeStaging, error) {
	id := uuid.NewString()
	stagingPath := filepath.Join(s.spoolDir, "exchange-"+id)
	if err := os.Mkdir(stagingPath, 0o700); err != nil {
		return nil, err
	}
	return &rawExchangeStaging{id: id, path: stagingPath}, nil
}

func (s rawExchangeSpool) createPart(staging *rawExchangeStaging, name string) (*os.File, error) {
	if staging == nil || !validRawExchangePart(name) {
		return nil, errors.New("invalid raw exchange staging part")
	}
	return os.OpenFile(filepath.Join(staging.path, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

func (s rawExchangeSpool) abort(staging *rawExchangeStaging) {
	_ = removeRawExchangeStaging(staging)
}

func removeRawExchangeStaging(staging *rawExchangeStaging) error {
	if staging == nil || staging.path == "" {
		return nil
	}
	if err := os.RemoveAll(staging.path); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(staging.path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	syncErr := parent.Sync()
	closeErr := parent.Close()
	return errors.Join(syncErr, closeErr)
}

func (s rawExchangeSpool) stagingForObjectKey(objectKey string) (*rawExchangeStaging, error) {
	id := path.Base(objectKey)
	if _, err := uuid.Parse(id); err != nil {
		return nil, errors.New("invalid raw exchange object key")
	}
	stagingPath := filepath.Join(s.spoolDir, "exchange-"+id)
	info, err := os.Stat(stagingPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("raw exchange staging path is not a directory")
	}
	return &rawExchangeStaging{id: id, path: stagingPath}, nil
}

func (s rawExchangeSpool) cleanupStale(before time.Time) (int, error) {
	entries, err := os.ReadDir(s.spoolDir)
	if err != nil {
		return 0, err
	}
	type staleSpoolEntry struct {
		path      string
		objectKey string
	}
	staleEntries := make([]staleSpoolEntry, 0)
	objectKeys := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "exchange-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(before) {
			continue
		}
		entryPath := filepath.Join(s.spoolDir, entry.Name())
		manifestData, err := os.ReadFile(filepath.Join(entryPath, rawExchangeManifestFileName))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		objectKey := ""
		if err == nil {
			var manifest rawExchangeManifest
			if err := common.Unmarshal(manifestData, &manifest); err != nil {
				return 0, fmt.Errorf("read stale raw exchange spool manifest: %w", err)
			}
			objectKey = manifest.ObjectKey
			if objectKey == "" && manifest.Archive != nil {
				objectKey = manifest.Archive.ObjectKey
			}
		}
		staleEntries = append(staleEntries, staleSpoolEntry{path: entryPath, objectKey: objectKey})
		if objectKey != "" {
			objectKeys = append(objectKeys, objectKey)
		}
	}
	pendingKeys, err := model.ExistingPendingRawExchangeObjectKeys(objectKeys)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, entry := range staleEntries {
		if _, pending := pendingKeys[entry.objectKey]; pending && entry.objectKey != "" {
			continue
		}
		if err := os.RemoveAll(entry.path); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (s *rawExchangeLocalStore) backend() string { return rawExchangeStorageBackendLocal }

func (s *rawExchangeLocalStore) ready() error {
	s.topologyMu.Lock()
	defer s.topologyMu.Unlock()
	if time.Since(s.topologyCheckedAt) < 10*time.Second {
		return s.topologyErr
	}
	s.topologyCheckedAt = time.Now()
	s.topologyErr = validateRawExchangeLocalTopology()
	return s.topologyErr
}

func (s *rawExchangeLocalStore) begin() (*rawExchangeStaging, error) {
	if s == nil {
		return nil, errors.New("raw exchange storage is unavailable")
	}
	return s.spool.begin()
}

func (s *rawExchangeLocalStore) createPart(staging *rawExchangeStaging, name string) (*os.File, error) {
	return s.spool.createPart(staging, name)
}

func (s *rawExchangeLocalStore) objectKey(staging *rawExchangeStaging, createdAt time.Time) (string, error) {
	if staging == nil || staging.id == "" {
		return "", errors.New("raw exchange staging is nil")
	}
	return path.Join(createdAt.UTC().Format("2006"), createdAt.UTC().Format("01"), createdAt.UTC().Format("02"), staging.id), nil
}

func (s *rawExchangeLocalStore) commit(staging *rawExchangeStaging, objectKey string) error {
	if staging == nil || staging.path == "" {
		return errors.New("raw exchange staging is nil")
	}
	destination, err := s.objectPath(objectKey)
	if err != nil {
		return err
	}
	destinationDir := filepath.Dir(destination)
	if err := os.MkdirAll(destinationDir, 0o700); err != nil {
		return err
	}
	if err := os.Rename(staging.path, destination); err != nil {
		return err
	}
	dir, err := os.Open(destinationDir)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
}

func (s *rawExchangeLocalStore) abort(staging *rawExchangeStaging) {
	s.spool.abort(staging)
}

func (s *rawExchangeLocalStore) open(objectKey, part string) (io.ReadCloser, int64, error) {
	if !validRawExchangePart(part) {
		return nil, 0, errors.New("invalid raw exchange part")
	}
	objectPath, err := s.objectPath(objectKey)
	if err != nil {
		return nil, 0, err
	}
	file, err := os.Open(filepath.Join(objectPath, part))
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	return file, info.Size(), nil
}

func (s *rawExchangeLocalStore) delete(objectKey string) error {
	objectPath, err := s.objectPath(objectKey)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(objectPath); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(objectPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	syncErr := parent.Sync()
	closeErr := parent.Close()
	return errors.Join(syncErr, closeErr)
}

func (s *rawExchangeLocalStore) partExists(objectKey, part string) (bool, error) {
	if !validRawExchangePart(part) {
		return false, errors.New("invalid raw exchange part")
	}
	objectPath, err := s.objectPath(objectKey)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(objectPath, part))
	if isRawExchangeObjectNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *rawExchangeLocalStore) scanCommittedObjects(cursor string, limit int) ([]rawExchangeCommittedObject, string, bool, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	byObjectKey := make(map[string]time.Time)
	err := filepath.WalkDir(s.objectsDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !validRawExchangePart(entry.Name()) {
			return nil
		}
		relative, err := filepath.Rel(s.objectsDir, filepath.Dir(filePath))
		if err != nil {
			return err
		}
		objectKey := filepath.ToSlash(relative)
		if objectKey <= cursor {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if current, exists := byObjectKey[objectKey]; !exists || info.ModTime().After(current) {
			byObjectKey[objectKey] = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return nil, cursor, false, err
	}
	objects := make([]rawExchangeCommittedObject, 0, len(byObjectKey))
	for objectKey, modifiedAt := range byObjectKey {
		objects = append(objects, rawExchangeCommittedObject{objectKey: objectKey, modifiedAt: modifiedAt})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].objectKey < objects[j].objectKey })
	if len(objects) <= limit {
		return objects, "", true, nil
	}
	objects = objects[:limit]
	return objects, objects[len(objects)-1].objectKey, false, nil
}

func (s *rawExchangeLocalStore) objectPath(objectKey string) (string, error) {
	if err := validateRawExchangeObjectKey(objectKey); err != nil {
		return "", err
	}
	objectPath := filepath.Join(s.objectsDir, filepath.FromSlash(objectKey))
	relative, err := filepath.Rel(s.objectsDir, objectPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("raw exchange object key escapes storage root")
	}
	return objectPath, nil
}

func (s *rawExchangeLocalStore) stagingForObjectKey(objectKey string) (*rawExchangeStaging, error) {
	return s.spool.stagingForObjectKey(objectKey)
}

func (s *rawExchangeLocalStore) cleanupStaleSpool(before time.Time) (int, error) {
	return s.spool.cleanupStale(before)
}

func newRawExchangeS3Store(spool rawExchangeSpool) (*rawExchangeS3Store, error) {
	if !common.NodeNameManuallyConfigured || strings.TrimSpace(common.NodeName) == "" {
		return nil, errors.New("S3 raw exchange storage requires a stable, explicit NODE_NAME")
	}
	bucket := strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_BUCKET"))
	if bucket == "" {
		return nil, errors.New("RAW_EXCHANGE_S3_BUCKET is required for S3 raw exchange storage")
	}
	region := strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_REGION"))
	if region == "" {
		region = "us-east-1"
	}
	accessKey := strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_ACCESS_KEY_ID"))
	secretKey := strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_SECRET_ACCESS_KEY"))
	if accessKey == "" || secretKey == "" {
		return nil, errors.New("RAW_EXCHANGE_S3_ACCESS_KEY_ID and RAW_EXCHANGE_S3_SECRET_ACCESS_KEY are required for S3 raw exchange storage")
	}
	prefix := strings.Trim(strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_PREFIX")), "/")
	if prefix == "" {
		prefix = "raw-exchanges"
	}
	if err := validateRawExchangeObjectKey(prefix); err != nil {
		return nil, fmt.Errorf("invalid RAW_EXCHANGE_S3_PREFIX: %w", err)
	}
	usePathStyle := true
	if configured := strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_USE_PATH_STYLE")); configured != "" {
		parsed, err := strconv.ParseBool(configured)
		if err != nil {
			return nil, fmt.Errorf("invalid RAW_EXCHANGE_S3_USE_PATH_STYLE: %w", err)
		}
		usePathStyle = parsed
	}

	options := s3.Options{
		Region:       region,
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKey, secretKey, strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_SESSION_TOKEN")))),
		UsePathStyle: usePathStyle,
	}
	if endpoint := strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_ENDPOINT")); endpoint != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, errors.New("RAW_EXCHANGE_S3_ENDPOINT must be an absolute HTTP or HTTPS URL")
		}
		options.BaseEndpoint = aws.String(strings.TrimRight(endpoint, "/"))
	}
	encryption := s3types.ServerSideEncryption("")
	switch configured := strings.ToLower(strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_SERVER_SIDE_ENCRYPTION"))); configured {
	case "":
	case "aes256":
		encryption = s3types.ServerSideEncryptionAes256
	case "aws:kms":
		encryption = s3types.ServerSideEncryptionAwsKms
	case "aws:kms:dsse":
		encryption = s3types.ServerSideEncryptionAwsKmsDsse
	default:
		return nil, errors.New("RAW_EXCHANGE_S3_SERVER_SIDE_ENCRYPTION must be AES256, aws:kms, or aws:kms:dsse")
	}
	kmsKeyId := strings.TrimSpace(os.Getenv("RAW_EXCHANGE_S3_SSE_KMS_KEY_ID"))
	if kmsKeyId != "" && encryption != s3types.ServerSideEncryptionAwsKms && encryption != s3types.ServerSideEncryptionAwsKmsDsse {
		return nil, errors.New("RAW_EXCHANGE_S3_SSE_KMS_KEY_ID requires aws:kms or aws:kms:dsse encryption")
	}
	store := &rawExchangeS3Store{
		spool:                spool,
		client:               s3.New(options),
		bucket:               bucket,
		prefix:               prefix,
		serverSideEncryption: encryption,
		kmsKeyId:             kmsKeyId,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := store.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); err != nil {
		return nil, fmt.Errorf("raw exchange S3 bucket is unavailable: %w", err)
	}
	if store.serverSideEncryption == "" {
		encryptionOutput, err := store.client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{Bucket: aws.String(bucket)})
		if err != nil {
			return nil, fmt.Errorf("raw exchange S3 requires default bucket encryption or RAW_EXCHANGE_S3_SERVER_SIDE_ENCRYPTION: %w", err)
		}
		encryptedByDefault := false
		if encryptionOutput.ServerSideEncryptionConfiguration != nil {
			for _, rule := range encryptionOutput.ServerSideEncryptionConfiguration.Rules {
				if rule.ApplyServerSideEncryptionByDefault != nil && rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm != "" {
					encryptedByDefault = true
					break
				}
			}
		}
		if !encryptedByDefault {
			return nil, errors.New("raw exchange S3 bucket does not have default server-side encryption")
		}
	}
	return store, nil
}

func (s *rawExchangeS3Store) backend() string { return rawExchangeStorageBackendS3 }
func (s *rawExchangeS3Store) ready() error    { return nil }

func (s *rawExchangeS3Store) begin() (*rawExchangeStaging, error) {
	if s == nil {
		return nil, errors.New("raw exchange storage is unavailable")
	}
	return s.spool.begin()
}

func (s *rawExchangeS3Store) createPart(staging *rawExchangeStaging, name string) (*os.File, error) {
	return s.spool.createPart(staging, name)
}

func (s *rawExchangeS3Store) objectKey(staging *rawExchangeStaging, createdAt time.Time) (string, error) {
	if staging == nil || staging.id == "" {
		return "", errors.New("raw exchange staging is nil")
	}
	return path.Join(s.prefix, createdAt.UTC().Format("2006"), createdAt.UTC().Format("01"), createdAt.UTC().Format("02"), staging.id), nil
}

func (s *rawExchangeS3Store) commit(staging *rawExchangeStaging, objectKey string) error {
	if staging == nil || staging.path == "" {
		return errors.New("raw exchange staging is nil")
	}
	if err := validateRawExchangeObjectKey(objectKey); err != nil {
		return err
	}
	for _, part := range []string{rawExchangeRequestFileName, rawExchangeResponseFileName, rawExchangeManifestFileName} {
		partPath := filepath.Join(staging.path, part)
		if part == rawExchangeResponseFileName {
			if _, err := os.Stat(partPath); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
		}
		if err := s.uploadPart(objectKey, part, partPath); err != nil {
			return err
		}
	}
	return nil
}

func (s *rawExchangeS3Store) uploadPart(objectKey, part, partPath string) error {
	file, err := os.Open(partPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	contentType := "application/octet-stream"
	if part == rawExchangeManifestFileName {
		contentType = "application/json"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	input := &s3.PutObjectInput{
		Bucket:               aws.String(s.bucket),
		Key:                  aws.String(path.Join(objectKey, part)),
		Body:                 file,
		CacheControl:         aws.String("no-store"),
		ContentLength:        aws.Int64(info.Size()),
		ContentType:          aws.String(contentType),
		ServerSideEncryption: s.serverSideEncryption,
	}
	if s.kmsKeyId != "" {
		input.SSEKMSKeyId = aws.String(s.kmsKeyId)
	}
	_, err = s.client.PutObject(ctx, input)
	return err
}

func (s *rawExchangeS3Store) abort(staging *rawExchangeStaging) {
	s.spool.abort(staging)
}

func (s *rawExchangeS3Store) open(objectKey, part string) (io.ReadCloser, int64, error) {
	if !validRawExchangePart(part) {
		return nil, 0, errors.New("invalid raw exchange part")
	}
	if err := validateRawExchangeObjectKey(objectKey); err != nil {
		return nil, 0, err
	}
	output, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path.Join(objectKey, part)),
	})
	if err != nil {
		return nil, 0, err
	}
	return output.Body, aws.ToInt64(output.ContentLength), nil
}

func (s *rawExchangeS3Store) delete(objectKey string) error {
	if err := validateRawExchangeObjectKey(objectKey); err != nil {
		return err
	}
	var result error
	for _, part := range []string{rawExchangeManifestFileName, rawExchangeRequestFileName, rawExchangeResponseFileName} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(path.Join(objectKey, part)),
		})
		cancel()
		if err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (s *rawExchangeS3Store) partExists(objectKey, part string) (bool, error) {
	if !validRawExchangePart(part) {
		return false, errors.New("invalid raw exchange part")
	}
	if err := validateRawExchangeObjectKey(objectKey); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path.Join(objectKey, part)),
	})
	if err == nil {
		return true, nil
	}
	if isRawExchangeObjectNotFound(err) {
		return false, nil
	}
	return false, err
}

func (s *rawExchangeS3Store) scanCommittedObjects(cursor string, limit int) ([]rawExchangeCommittedObject, string, bool, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		Prefix:  aws.String(s.prefix + "/"),
		MaxKeys: aws.Int32(int32(limit)),
	}
	if cursor != "" {
		input.ContinuationToken = aws.String(cursor)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := s.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, cursor, false, err
	}
	byObjectKey := make(map[string]time.Time)
	for _, object := range output.Contents {
		key := aws.ToString(object.Key)
		objectKey := ""
		for _, part := range []string{rawExchangeManifestFileName, rawExchangeRequestFileName, rawExchangeResponseFileName} {
			suffix := "/" + part
			if strings.HasSuffix(key, suffix) {
				objectKey = strings.TrimSuffix(key, suffix)
				break
			}
		}
		if objectKey == "" {
			continue
		}
		modifiedAt := aws.ToTime(object.LastModified)
		if current, exists := byObjectKey[objectKey]; !exists || modifiedAt.After(current) {
			byObjectKey[objectKey] = modifiedAt
		}
	}
	objects := make([]rawExchangeCommittedObject, 0, len(byObjectKey))
	for objectKey, modifiedAt := range byObjectKey {
		objects = append(objects, rawExchangeCommittedObject{objectKey: objectKey, modifiedAt: modifiedAt})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].objectKey < objects[j].objectKey })
	return objects, aws.ToString(output.NextContinuationToken), !aws.ToBool(output.IsTruncated), nil
}

func (s *rawExchangeS3Store) stagingForObjectKey(objectKey string) (*rawExchangeStaging, error) {
	if err := validateRawExchangeObjectKey(objectKey); err != nil {
		return nil, err
	}
	return s.spool.stagingForObjectKey(objectKey)
}

func (s *rawExchangeS3Store) cleanupStaleSpool(before time.Time) (int, error) {
	return s.spool.cleanupStale(before)
}

func isRawExchangeObjectNotFound(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var responseErr *smithyhttp.ResponseError
	return errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == 404
}

func validateRawExchangeObjectKey(objectKey string) error {
	if objectKey == "" || strings.HasPrefix(objectKey, "/") || path.Clean(objectKey) != objectKey || objectKey == "." || objectKey == ".." || strings.HasPrefix(objectKey, "../") {
		return errors.New("invalid raw exchange object key")
	}
	return nil
}

func validRawExchangePart(part string) bool {
	switch part {
	case rawExchangeRequestFileName, rawExchangeResponseFileName, rawExchangeManifestFileName:
		return true
	default:
		return false
	}
}
