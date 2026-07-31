package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

const (
	rawExchangeCaptureSessionKey = "raw_exchange_capture_session"
	maxRawExchangeSSELineBytes   = 64 << 10
)

type RawExchangeCaptureSession struct {
	mu sync.Mutex

	store        rawExchangeStore
	staging      *rawExchangeStaging
	responseFile *os.File
	responseHash hash.Hash

	mode                   string
	requestId              string
	userId                 int
	tokenId                int
	createdAt              time.Time
	requestContentType     string
	requestContentEncoding string
	requestBytes           int64
	responseBytes          int64
	requestStoredBytes     int64
	responseStoredBytes    int64
	requestSha256          string
	responseSha256         string

	maxRequestBytes  int64
	maxResponseBytes int64
	maxExchangeBytes int64
	capacityBytes    int64
	retentionDays    int

	captureStatus string
	captureError  string
	writeFailed   bool
	responseLines rawExchangeSSEObserver
	relayInfo     *relaycommon.RelayInfo
	finalized     bool
}

type rawExchangeSSEObserver struct {
	line               []byte
	overlongLine       bool
	currentEvent       string
	done               bool
	responsesCompleted bool
	responsesFailed    bool
	terminal           bool
}

type rawExchangeSSEEvent struct {
	Type     string `json:"type"`
	Status   string `json:"status"`
	Response *struct {
		Status string `json:"status"`
	} `json:"response"`
}

type rawExchangeManifest struct {
	Version   int                       `json:"version"`
	ObjectKey string                    `json:"object_key"`
	Archive   *model.RawExchangeArchive `json:"archive"`
}

func RawExchangeStorageReady() error {
	store, err := rawExchangeStoreSnapshot()
	if err != nil {
		return err
	}
	return store.ready()
}

func RawExchangeCaptureReady() error {
	if !system_setting.GetRawExchangeSettings().Enabled {
		return errors.New("raw exchange capture is disabled")
	}
	return RawExchangeStorageReady()
}

func StartRawExchangeCapture(c *gin.Context, mode string) *RawExchangeCaptureSession {
	if c == nil || mode == model.RawExchangeCaptureOff {
		return nil
	}
	settings := *system_setting.GetRawExchangeSettings()
	if !settings.Enabled {
		return nil
	}
	store, err := rawExchangeStoreSnapshot()
	if err == nil {
		err = store.ready()
	}
	if err != nil {
		logger.LogWarn(c, "raw exchange capture unavailable: "+err.Error())
		return nil
	}

	now := time.Now()
	session := &RawExchangeCaptureSession{
		store:                  store,
		mode:                   mode,
		requestId:              c.GetString(common.RequestIdKey),
		userId:                 c.GetInt(string(constant.ContextKeyUserId)),
		tokenId:                c.GetInt(string(constant.ContextKeyTokenId)),
		createdAt:              now,
		requestContentType:     truncateRawExchangeMetadata(c.GetHeader("Content-Type"), 255),
		requestContentEncoding: truncateRawExchangeMetadata(c.GetString(string(constant.ContextKeyOriginalContentEncoding)), 64),
		maxRequestBytes:        positiveRawExchangeLimit(settings.MaxRequestBytes, 16<<20),
		maxResponseBytes:       positiveRawExchangeLimit(settings.MaxResponseBytes, 32<<20),
		maxExchangeBytes:       positiveRawExchangeLimit(settings.MaxExchangeBytes, 48<<20),
		capacityBytes:          positiveRawExchangeLimit(settings.GlobalCapacityBytes, 10<<30),
		retentionDays:          settings.RetentionDays,
	}
	if session.requestId == "" {
		session.requestId = common.NewRequestId()
	}
	if session.retentionDays <= 0 {
		session.retentionDays = 7
	}
	c.Set(rawExchangeCaptureSessionKey, session)

	bodyStorage, bodyErr := common.GetBodyStorage(c)
	if bodyErr != nil {
		session.fail(model.RawExchangeStatusCaptureFailed, "request_read_failed")
		return session
	}
	session.requestBytes = bodyStorage.Size()
	if session.requestBytes > session.maxRequestBytes || session.requestBytes > session.maxExchangeBytes {
		session.fail(model.RawExchangeStatusSkippedTooLarge, "request_too_large")
		resetRawExchangeRequestBody(c, bodyStorage)
		return session
	}

	staging, stageErr := store.begin()
	if stageErr != nil {
		session.fail(model.RawExchangeStatusCaptureFailed, "spool_create_failed")
		resetRawExchangeRequestBody(c, bodyStorage)
		return session
	}
	session.staging = staging
	requestFile, fileErr := store.createPart(staging, rawExchangeRequestFileName)
	if fileErr != nil {
		session.fail(model.RawExchangeStatusCaptureFailed, "request_file_create_failed")
		resetRawExchangeRequestBody(c, bodyStorage)
		return session
	}

	requestHash := sha256.New()
	if _, seekErr := bodyStorage.Seek(0, io.SeekStart); seekErr != nil {
		_ = requestFile.Close()
		session.fail(model.RawExchangeStatusCaptureFailed, "request_seek_failed")
		resetRawExchangeRequestBody(c, bodyStorage)
		return session
	}
	written, copyErr := io.Copy(io.MultiWriter(requestFile, requestHash), bodyStorage)
	syncErr := requestFile.Sync()
	closeErr := requestFile.Close()
	resetRawExchangeRequestBody(c, bodyStorage)
	if copyErr != nil || syncErr != nil || closeErr != nil || written != session.requestBytes {
		session.fail(model.RawExchangeStatusCaptureFailed, "request_spool_write_failed")
		return session
	}
	session.requestStoredBytes = written
	session.requestSha256 = hex.EncodeToString(requestHash.Sum(nil))
	return session
}

func AttachRawExchangeRelayInfo(c *gin.Context, info *relaycommon.RelayInfo) {
	if session := rawExchangeCaptureSession(c); session != nil {
		session.mu.Lock()
		session.relayInfo = info
		session.mu.Unlock()
	}
}

func CaptureRawExchangeResponse(c *gin.Context, data []byte, accepted int, writeErr error) {
	session := rawExchangeCaptureSession(c)
	if session == nil {
		return
	}
	session.captureResponse(data, accepted, writeErr)
}

func FinalizeRawExchangeCapture(c *gin.Context) {
	session := rawExchangeCaptureSession(c)
	if session == nil {
		return
	}
	if err := session.finalize(c); err != nil {
		logger.LogWarn(c, "raw exchange finalization failed: "+err.Error())
	}
}

func rawExchangeCaptureSession(c *gin.Context) *RawExchangeCaptureSession {
	if c == nil {
		return nil
	}
	value, ok := c.Get(rawExchangeCaptureSessionKey)
	if !ok {
		return nil
	}
	session, _ := value.(*RawExchangeCaptureSession)
	return session
}

func (s *RawExchangeCaptureSession) captureResponse(data []byte, accepted int, writeErr error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if accepted < 0 {
		accepted = 0
	}
	if accepted > len(data) {
		accepted = len(data)
	}
	acceptedData := data[:accepted]
	s.responseBytes = saturatingRawExchangeAdd(s.responseBytes, int64(accepted))
	s.responseLines.observe(acceptedData)
	if writeErr != nil || accepted != len(data) {
		s.writeFailed = true
	}
	if accepted == 0 || s.captureStatus != "" {
		return
	}
	if s.responseBytes > s.maxResponseBytes || s.requestBytes > s.maxExchangeBytes-s.responseBytes {
		s.failLocked(model.RawExchangeStatusSkippedTooLarge, "response_too_large")
		return
	}
	if s.responseFile == nil {
		file, err := s.store.createPart(s.staging, rawExchangeResponseFileName)
		if err != nil {
			s.failLocked(model.RawExchangeStatusCaptureFailed, "response_file_create_failed")
			return
		}
		s.responseFile = file
		s.responseHash = sha256.New()
	}
	written, err := io.MultiWriter(s.responseFile, s.responseHash).Write(acceptedData)
	s.responseStoredBytes = saturatingRawExchangeAdd(s.responseStoredBytes, int64(written))
	if err != nil || written != len(acceptedData) {
		s.failLocked(model.RawExchangeStatusCaptureFailed, "response_spool_write_failed")
	}
}

func (s *RawExchangeCaptureSession) finalize(c *gin.Context) error {
	s.mu.Lock()
	if s.finalized {
		s.mu.Unlock()
		return nil
	}
	s.finalized = true
	if s.responseFile != nil {
		syncErr := s.responseFile.Sync()
		closeErr := s.responseFile.Close()
		s.responseFile = nil
		if syncErr != nil || closeErr != nil {
			s.failLocked(model.RawExchangeStatusCaptureFailed, "response_spool_close_failed")
		}
	}
	if s.responseHash != nil && s.captureStatus == "" {
		s.responseSha256 = hex.EncodeToString(s.responseHash.Sum(nil))
	}
	outcome, reason, responseComplete := s.classifyLocked(c)
	archive := s.archiveLocked(c, outcome, reason, responseComplete)
	mode := s.mode
	captureStatus := s.captureStatus
	captureError := s.captureError
	staging := s.staging
	store := s.store
	s.mu.Unlock()

	preserveStaging := false
	if staging != nil {
		defer func() {
			if !preserveStaging {
				store.abort(staging)
			}
		}()
	}
	if mode == model.RawExchangeCaptureNonSuccess && outcome == model.RawExchangeOutcomeSuccess {
		return nil
	}
	if captureStatus != "" {
		archive.Status = captureStatus
		archive.ErrorCode = captureError
		archive.RequestAvailable = false
		archive.ResponseAvailable = false
		archive.RequestStoredBytes = 0
		archive.ResponseStoredBytes = 0
		return model.CreateRawExchangeArchive(archive, s.capacityBytes)
	}
	if staging == nil {
		archive.Status = model.RawExchangeStatusCaptureFailed
		archive.ErrorCode = "spool_missing"
		return model.CreateRawExchangeArchive(archive, s.capacityBytes)
	}

	objectKey, err := store.objectKey(staging, s.createdAt)
	if err != nil {
		return s.recordCommitFailure(archive, "object_key_failed", err)
	}
	archive.ObjectKey = objectKey
	if store.backend() == rawExchangeStorageBackendS3 {
		archive.Status = model.RawExchangeStatusPendingCommit
		if err := writeRawExchangeManifest(store, staging, archive); err != nil {
			return s.recordCommitFailure(archive, "manifest_write_failed", err)
		}
		if err := model.CreateRawExchangeArchive(archive, s.capacityBytes); err != nil {
			return err
		}
		preserveStaging = true
		enqueueRawExchangeCommit(archive.Id)
		return nil
	}

	archive.Status = model.RawExchangeStatusStored
	archive.CommittedAt = common.GetTimestamp()
	if err := writeRawExchangeManifest(store, staging, archive); err != nil {
		return s.recordCommitFailure(archive, "manifest_write_failed", err)
	}
	if err := store.commit(staging, objectKey); err != nil {
		cleanupErr := store.delete(objectKey)
		return s.recordCommitFailure(archive, "object_commit_failed", errors.Join(err, cleanupErr))
	}
	if err := model.CreateRawExchangeArchive(archive, s.capacityBytes); err != nil {
		deleteErr := store.delete(objectKey)
		if errors.Is(err, model.ErrRawExchangeCapacityExceeded) {
			archive.Id = 0
			archive.Status = model.RawExchangeStatusSkippedCapacity
			archive.ErrorCode = "capacity_exceeded"
			archive.ObjectKey = ""
			archive.RequestAvailable = false
			archive.ResponseAvailable = false
			archive.RequestStoredBytes = 0
			archive.ResponseStoredBytes = 0
			return errors.Join(model.CreateRawExchangeArchive(archive, s.capacityBytes), deleteErr)
		}
		return errors.Join(err, deleteErr)
	}
	return nil
}

func (s *RawExchangeCaptureSession) archiveLocked(c *gin.Context, outcome, reason string, responseComplete bool) *model.RawExchangeArchive {
	status := c.Writer.Status()
	if status == 0 {
		status = 200
	}
	expiresAt := s.createdAt.Add(time.Duration(s.retentionDays) * 24 * time.Hour).Unix()
	return &model.RawExchangeArchive{
		RequestId:               s.requestId,
		UserId:                  s.userId,
		TokenId:                 s.tokenId,
		CaptureMode:             s.mode,
		Outcome:                 outcome,
		OutcomeReason:           truncateRawExchangeMetadata(reason, 64),
		Status:                  model.RawExchangeStatusStored,
		HttpStatus:              status,
		RequestMethod:           truncateRawExchangeMetadata(strings.ToUpper(c.Request.Method), 16),
		RequestPath:             truncateRawExchangeMetadata(c.Request.URL.EscapedPath(), 512),
		RequestAvailable:        s.requestStoredBytes == s.requestBytes,
		ResponseAvailable:       s.responseStoredBytes > 0,
		ResponseComplete:        responseComplete,
		RequestContentType:      s.requestContentType,
		ResponseContentType:     truncateRawExchangeMetadata(c.Writer.Header().Get("Content-Type"), 255),
		RequestContentEncoding:  s.requestContentEncoding,
		ResponseContentEncoding: truncateRawExchangeMetadata(c.Writer.Header().Get("Content-Encoding"), 64),
		RequestBytes:            s.requestBytes,
		ResponseBytes:           s.responseBytes,
		RequestStoredBytes:      s.requestStoredBytes,
		ResponseStoredBytes:     s.responseStoredBytes,
		RequestSha256:           s.requestSha256,
		ResponseSha256:          s.responseSha256,
		StorageBackend:          s.store.backend(),
		NodeName:                truncateRawExchangeMetadata(common.NodeName, 128),
		CreatedAt:               s.createdAt.Unix(),
		ExpiresAt:               expiresAt,
	}
}

func (s *RawExchangeCaptureSession) classifyLocked(c *gin.Context) (string, string, bool) {
	contentType := c.Writer.Header().Get("Content-Type")
	isEventStream := strings.Contains(strings.ToLower(contentType), "text/event-stream")
	if c.Request.Context().Err() != nil {
		return model.RawExchangeOutcomeNonSuccess, "client_cancelled", false
	}
	if s.writeFailed {
		return model.RawExchangeOutcomeNonSuccess, "response_write_failed", false
	}
	status := c.Writer.Status()
	if status < 200 || status >= 300 {
		return model.RawExchangeOutcomeNonSuccess, "http_status", !isEventStream || s.responseLines.terminal
	}
	if !isEventStream && (s.relayInfo == nil || !s.relayInfo.IsStream) {
		return model.RawExchangeOutcomeSuccess, "", true
	}
	if s.responseLines.responsesFailed {
		return model.RawExchangeOutcomeNonSuccess, "responses_failed", s.responseLines.terminal
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/responses") {
		if s.responseLines.responsesCompleted {
			return model.RawExchangeOutcomeSuccess, "", true
		}
		return model.RawExchangeOutcomeNonSuccess, "incomplete_stream", false
	}
	if s.relayInfo != nil && s.relayInfo.StreamStatus != nil {
		streamStatus := s.relayInfo.StreamStatus
		if streamStatus.HasErrors() {
			return model.RawExchangeOutcomeNonSuccess, "stream_error", streamStatus.IsNormalEnd()
		}
		if streamStatus.IsNormalEnd() {
			if streamStatus.EndError == nil {
				return model.RawExchangeOutcomeSuccess, "", true
			}
			return model.RawExchangeOutcomeNonSuccess, "stream_handler_stopped", true
		}
		reason := string(streamStatus.EndReason)
		if reason == "" {
			reason = "incomplete_stream"
		}
		return model.RawExchangeOutcomeNonSuccess, reason, false
	}
	if s.responseLines.done {
		return model.RawExchangeOutcomeSuccess, "", true
	}
	return model.RawExchangeOutcomeNonSuccess, "incomplete_stream", false
}

func writeRawExchangeManifest(store rawExchangeStore, staging *rawExchangeStaging, archive *model.RawExchangeArchive) error {
	manifestData, err := common.Marshal(rawExchangeManifest{Version: 1, ObjectKey: archive.ObjectKey, Archive: archive})
	if err != nil {
		return err
	}
	manifestFile, err := store.createPart(staging, rawExchangeManifestFileName)
	if err != nil {
		return err
	}
	written, writeErr := manifestFile.Write(manifestData)
	syncErr := manifestFile.Sync()
	closeErr := manifestFile.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || written != len(manifestData) {
		return errors.Join(writeErr, syncErr, closeErr)
	}
	return nil
}

func (s *RawExchangeCaptureSession) recordCommitFailure(archive *model.RawExchangeArchive, code string, cause error) error {
	archive.Status = model.RawExchangeStatusCaptureFailed
	archive.ErrorCode = code
	archive.RequestAvailable = false
	archive.ResponseAvailable = false
	archive.RequestStoredBytes = 0
	archive.ResponseStoredBytes = 0
	archive.ObjectKey = ""
	if err := model.CreateRawExchangeArchive(archive, s.capacityBytes); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (s *RawExchangeCaptureSession) fail(status, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failLocked(status, code)
}

func (s *RawExchangeCaptureSession) failLocked(status, code string) {
	if s.captureStatus != "" {
		return
	}
	s.captureStatus = status
	s.captureError = code
	if s.responseFile != nil {
		_ = s.responseFile.Close()
		s.responseFile = nil
	}
}

func (o *rawExchangeSSEObserver) observe(data []byte) {
	for _, b := range data {
		if b == '\n' {
			if !o.overlongLine {
				o.processLine(strings.TrimSuffix(string(o.line), "\r"))
			}
			o.line = o.line[:0]
			o.overlongLine = false
			continue
		}
		if o.overlongLine {
			continue
		}
		if len(o.line) >= maxRawExchangeSSELineBytes {
			o.line = o.line[:0]
			o.overlongLine = true
			continue
		}
		o.line = append(o.line, b)
	}
}

func (o *rawExchangeSSEObserver) processLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		o.currentEvent = ""
		return
	}
	if strings.HasPrefix(line, "event:") {
		o.currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		switch o.currentEvent {
		case "response.failed", "response.error", "response.incomplete", "response.cancelled", "response.canceled", "error":
			o.responsesFailed = true
			o.terminal = true
		}
		return
	}
	if !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "[DONE]" {
		o.done = true
		o.terminal = true
		return
	}
	var event rawExchangeSSEEvent
	if err := common.UnmarshalJsonStr(payload, &event); err != nil {
		return
	}
	eventType := o.currentEvent
	if eventType == "" {
		eventType = event.Type
	}
	switch eventType {
	case "response.completed", "response.done":
		status := event.Status
		if event.Response != nil {
			status = event.Response.Status
		}
		if status == "completed" {
			o.responsesCompleted = true
			o.terminal = true
		} else if status != "" {
			o.responsesFailed = true
			o.terminal = true
		}
	case "response.failed", "response.error", "response.incomplete", "response.cancelled", "response.canceled", "error":
		o.responsesFailed = true
		o.terminal = true
	}
}

func resetRawExchangeRequestBody(c *gin.Context, storage common.BodyStorage) {
	if storage == nil {
		return
	}
	if _, err := storage.Seek(0, io.SeekStart); err == nil {
		c.Request.Body = io.NopCloser(storage)
	}
}

func positiveRawExchangeLimit(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func saturatingRawExchangeAdd(current, increment int64) int64 {
	if increment <= 0 {
		return current
	}
	if current > math.MaxInt64-increment {
		return math.MaxInt64
	}
	return current + increment
}

func truncateRawExchangeMetadata(value string, maxBytes int) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut]
}

func (s *RawExchangeCaptureSession) String() string {
	if s == nil {
		return "RawExchangeCaptureSession<nil>"
	}
	return fmt.Sprintf("RawExchangeCaptureSession<request_id=%s mode=%s>", s.requestId, s.mode)
}
