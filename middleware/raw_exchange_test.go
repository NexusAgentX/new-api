package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	rawExchangeRequestFileName  = "request.bin"
	rawExchangeResponseFileName = "response.bin"
)

func TestRawExchangeCaptureSupportedRoutesStayWithinTextBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		expected    bool
	}{
		{name: "chat JSON", method: http.MethodPost, path: "/v1/chat/completions", contentType: "application/json", expected: true},
		{name: "responses SSE request", method: http.MethodPost, path: "/v1/responses", contentType: "application/json", expected: true},
		{name: "Gemini generate", method: http.MethodPost, path: "/v1beta/models/gemini:generateContent", contentType: "application/json", expected: true},
		{name: "Gemini stream", method: http.MethodPost, path: "/v1beta/models/gemini:streamGenerateContent", contentType: "application/json", expected: true},
		{name: "Gemini async video", method: http.MethodPost, path: "/v1beta/models/veo:predictLongRunning", contentType: "application/json", expected: false},
		{name: "image generation", method: http.MethodPost, path: "/v1/images/generations", contentType: "application/json", expected: false},
		{name: "audio binary", method: http.MethodPost, path: "/v1/audio/speech", contentType: "application/json", expected: false},
		{name: "multipart", method: http.MethodPost, path: "/v1/chat/completions", contentType: "multipart/form-data; boundary=test", expected: false},
		{name: "WebSocket realtime", method: http.MethodGet, path: "/v1/realtime", expected: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(test.method, test.path, nil)
			if test.contentType != "" {
				context.Request.Header.Set("Content-Type", test.contentType)
			}
			assert.Equal(t, test.expected, rawExchangeCaptureSupported(context))
		})
	}
}

func setupRawExchangeMiddlewareTest(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousMainType := common.MainDatabaseType()
	previousLogType := common.LogDatabaseType()
	previousSettings := *system_setting.GetRawExchangeSettings()
	previousPath := os.Getenv("RAW_EXCHANGE_STORAGE_PATH")
	previousBackend := os.Getenv("RAW_EXCHANGE_STORAGE_BACKEND")

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.RawExchangeArchive{}, &model.RawExchangeStorageUsage{}))
	require.NoError(t, model.InitializeRawExchangeStorageUsage())

	root := t.TempDir()
	require.NoError(t, os.Setenv("RAW_EXCHANGE_STORAGE_PATH", root))
	require.NoError(t, os.Setenv("RAW_EXCHANGE_STORAGE_BACKEND", "local"))
	require.NoError(t, service.InitRawExchangeStorage())
	settings := system_setting.GetRawExchangeSettings()
	settings.Enabled = true
	settings.MaxRequestBytes = 1 << 20
	settings.MaxResponseBytes = 1 << 20
	settings.MaxExchangeBytes = 2 << 20
	settings.GlobalCapacityBytes = 8 << 20
	settings.RetentionDays = 7

	t.Cleanup(func() {
		*system_setting.GetRawExchangeSettings() = previousSettings
		_ = os.Setenv("RAW_EXCHANGE_STORAGE_PATH", previousPath)
		_ = os.Setenv("RAW_EXCHANGE_STORAGE_BACKEND", previousBackend)
		_ = service.InitRawExchangeStorage()
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db, root
}

func newRawExchangeTestRouter(mode string, path string, handler gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(RawExchangeLifecycle())
	router.Use(gin.CustomRecovery(func(c *gin.Context, recovered any) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprint(recovered)})
	}))
	router.Use(RequestId())
	router.Use(DecompressRequestMiddleware())
	router.Use(BodyStorageCleanup())
	router.POST(path,
		func(c *gin.Context) {
			common.SetContextKey(c, constant.ContextKeyUserId, 11)
			common.SetContextKey(c, constant.ContextKeyTokenId, 22)
			common.SetContextKey(c, constant.ContextKeyTokenRawExchangeCaptureMode, mode)
			c.Next()
		},
		RawExchangeCapture(),
		handler,
	)
	return router
}

func performRawExchangeRequest(t *testing.T, router *gin.Engine, path string, body []byte, encoding string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if encoding != "" {
		request.Header.Set("Content-Encoding", encoding)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func loadSingleRawExchangeArchive(t *testing.T, db *gorm.DB) model.RawExchangeArchive {
	t.Helper()
	var archives []model.RawExchangeArchive
	require.NoError(t, db.Find(&archives).Error)
	require.Len(t, archives, 1)
	return archives[0]
}

func readRawExchangePart(t *testing.T, root string, archive model.RawExchangeArchive, part string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "objects", archive.ObjectKey, part))
	require.NoError(t, err)
	return data
}

func TestRawExchangeCaptureSizeFailureDoesNotChangeRelayResponse(t *testing.T) {
	tests := []struct {
		name              string
		maxRequestBytes   int64
		maxResponseBytes  int64
		maxExchangeBytes  int64
		globalCapacity    int64
		requestBody       []byte
		responseBody      string
		expectedStatus    string
		expectedErrorCode string
	}{
		{
			name:              "request too large",
			maxRequestBytes:   8,
			maxResponseBytes:  1 << 20,
			maxExchangeBytes:  1 << 20,
			globalCapacity:    1 << 20,
			requestBody:       []byte(`{"model":"larger-than-eight"}`),
			responseBody:      "relay response",
			expectedStatus:    model.RawExchangeStatusSkippedTooLarge,
			expectedErrorCode: "request_too_large",
		},
		{
			name:              "response too large",
			maxRequestBytes:   1 << 20,
			maxResponseBytes:  8,
			maxExchangeBytes:  1 << 20,
			globalCapacity:    1 << 20,
			requestBody:       []byte(`{"model":"small"}`),
			responseBody:      "response-larger-than-eight",
			expectedStatus:    model.RawExchangeStatusSkippedTooLarge,
			expectedErrorCode: "response_too_large",
		},
		{
			name:              "global capacity",
			maxRequestBytes:   1 << 20,
			maxResponseBytes:  1 << 20,
			maxExchangeBytes:  1 << 20,
			globalCapacity:    1,
			requestBody:       []byte(`{"model":"small"}`),
			responseBody:      "relay response",
			expectedStatus:    model.RawExchangeStatusSkippedCapacity,
			expectedErrorCode: "capacity_exceeded",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := setupRawExchangeMiddlewareTest(t)
			settings := system_setting.GetRawExchangeSettings()
			settings.MaxRequestBytes = test.maxRequestBytes
			settings.MaxResponseBytes = test.maxResponseBytes
			settings.MaxExchangeBytes = test.maxExchangeBytes
			settings.GlobalCapacityBytes = test.globalCapacity
			router := newRawExchangeTestRouter(model.RawExchangeCaptureAll, "/v1/chat/completions", func(c *gin.Context) {
				c.String(http.StatusOK, test.responseBody)
			})

			recorder := performRawExchangeRequest(t, router, "/v1/chat/completions", test.requestBody, "")
			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, test.responseBody, recorder.Body.String())
			archive := loadSingleRawExchangeArchive(t, db)
			assert.Equal(t, test.expectedStatus, archive.Status)
			assert.Equal(t, test.expectedErrorCode, archive.ErrorCode)
			assert.False(t, archive.RequestAvailable)
			assert.False(t, archive.ResponseAvailable)
		})
	}
}

func TestRawExchangeCaptureFailurePreservesRelayWriterAndBillingContract(t *testing.T) {
	db, root := setupRawExchangeMiddlewareTest(t)
	attempts := 0
	settledQuota := 0
	router := newRawExchangeTestRouter(model.RawExchangeCaptureAll, "/v1/chat/completions", func(c *gin.Context) {
		entries, err := os.ReadDir(filepath.Join(root, "spool"))
		require.NoError(t, err)
		require.Len(t, entries, 1)
		stagingPath := filepath.Join(root, "spool", entries[0].Name())
		require.NoError(t, os.Chmod(stagingPath, 0o500))
		defer func() { require.NoError(t, os.Chmod(stagingPath, 0o700)) }()

		attempts++
		written, writeErr := c.Writer.WriteString("relay response")
		assert.NoError(t, writeErr)
		assert.Equal(t, len("relay response"), written)
		c.Writer.Flush()
		settledQuota += 37
	})

	recorder := performRawExchangeRequest(t, router, "/v1/chat/completions", []byte(`{"model":"gpt-test"}`), "")
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "relay response", recorder.Body.String())
	assert.True(t, recorder.Flushed)
	assert.Equal(t, 1, attempts)
	assert.Equal(t, 37, settledQuota)
	archive := loadSingleRawExchangeArchive(t, db)
	assert.Equal(t, model.RawExchangeStatusCaptureFailed, archive.Status)
	assert.Equal(t, "response_file_create_failed", archive.ErrorCode)
}

func TestRawExchangeClientCancellationPreservesAcceptedResponseBytes(t *testing.T) {
	db, root := setupRawExchangeMiddlewareTest(t)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureAll, "/v1/chat/completions", func(c *gin.Context) {
		_, err := c.Writer.WriteString("accepted before cancellation")
		require.NoError(t, err)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-test"}`))
	request.Header.Set("Content-Type", "application/json")
	cancelledContext, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(cancelledContext)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "accepted before cancellation", recorder.Body.String())
	archive := loadSingleRawExchangeArchive(t, db)
	assert.Equal(t, model.RawExchangeOutcomeNonSuccess, archive.Outcome)
	assert.Equal(t, "client_cancelled", archive.OutcomeReason)
	assert.False(t, archive.ResponseComplete)
	assert.Equal(t, []byte("accepted before cancellation"), readRawExchangePart(t, root, archive, rawExchangeResponseFileName))
}

func TestRawExchangeCaptureStoresRequestBeforeCustomization(t *testing.T) {
	db, root := setupRawExchangeMiddlewareTest(t)
	original := []byte(`{"model":"client-model","input":"hello"}`)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureAll, "/v1/responses", func(c *gin.Context) {
		require.NoError(t, common.ReplaceBodyStorage(c, []byte(`{"model":"upstream-model"}`)))
		c.JSON(http.StatusOK, gin.H{"result": "ok"})
	})

	recorder := performRawExchangeRequest(t, router, "/v1/responses?key=must-not-be-archived", original, "")
	require.Equal(t, http.StatusOK, recorder.Code)
	archive := loadSingleRawExchangeArchive(t, db)
	assert.Equal(t, http.MethodPost, archive.RequestMethod)
	assert.Equal(t, "/v1/responses", archive.RequestPath)
	assert.NotContains(t, archive.RequestPath, "must-not-be-archived")
	assert.Equal(t, model.RawExchangeOutcomeSuccess, archive.Outcome)
	assert.True(t, archive.ResponseComplete)
	assert.Equal(t, original, readRawExchangePart(t, root, archive, rawExchangeRequestFileName))
	assert.JSONEq(t, `{"result":"ok"}`, string(readRawExchangePart(t, root, archive, rawExchangeResponseFileName)))
}

func TestRawExchangeCaptureStoresDecompressedRequestAndOriginalEncoding(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		compress func(*testing.T, []byte) []byte
	}{
		{
			name:     "gzip",
			encoding: "gzip",
			compress: func(t *testing.T, data []byte) []byte {
				t.Helper()
				var compressed bytes.Buffer
				writer := gzip.NewWriter(&compressed)
				_, err := writer.Write(data)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				return compressed.Bytes()
			},
		},
		{
			name:     "brotli",
			encoding: "br",
			compress: func(t *testing.T, data []byte) []byte {
				t.Helper()
				var compressed bytes.Buffer
				writer := brotli.NewWriter(&compressed)
				_, err := writer.Write(data)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				return compressed.Bytes()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, root := setupRawExchangeMiddlewareTest(t)
			original := []byte(`{"model":"gpt-test"}`)
			router := newRawExchangeTestRouter(model.RawExchangeCaptureAll, "/v1/chat/completions", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			performRawExchangeRequest(t, router, "/v1/chat/completions", test.compress(t, original), test.encoding)
			archive := loadSingleRawExchangeArchive(t, db)
			assert.Equal(t, test.encoding, archive.RequestContentEncoding)
			assert.Equal(t, original, readRawExchangePart(t, root, archive, rawExchangeRequestFileName))
		})
	}
}

func TestRawExchangeNonSuccessDiscardsSuccessfulExchange(t *testing.T) {
	db, root := setupRawExchangeMiddlewareTest(t)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureNonSuccess, "/v1/chat/completions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	performRawExchangeRequest(t, router, "/v1/chat/completions", []byte(`{"model":"gpt-test"}`), "")
	var count int64
	require.NoError(t, db.Model(&model.RawExchangeArchive{}).Count(&count).Error)
	assert.Zero(t, count)
	entries, err := os.ReadDir(filepath.Join(root, "spool"))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestRawExchangeNonSuccessStoresHTTPFailure(t *testing.T) {
	db, root := setupRawExchangeMiddlewareTest(t)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureNonSuccess, "/v1/chat/completions", func(c *gin.Context) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream failed"})
	})

	recorder := performRawExchangeRequest(t, router, "/v1/chat/completions", []byte(`{"model":"gpt-test"}`), "")
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	archive := loadSingleRawExchangeArchive(t, db)
	assert.Equal(t, model.RawExchangeOutcomeNonSuccess, archive.Outcome)
	assert.Equal(t, "http_status", archive.OutcomeReason)
	assert.True(t, archive.ResponseComplete)
	assert.JSONEq(t, `{"error":"upstream failed"}`, string(readRawExchangePart(t, root, archive, rawExchangeResponseFileName)))
}

func TestRawExchangeNonSuccessStoresIncompleteResponsesStream(t *testing.T) {
	db, _ := setupRawExchangeMiddlewareTest(t)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureNonSuccess, "/v1/responses", func(c *gin.Context) {
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
		service.AttachRawExchangeRelayInfo(c, &relaycommon.RelayInfo{IsStream: true, StreamStatus: status})
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("event: response.output_text.delta\ndata: {\"delta\":\"partial\"}\n\n")
	})

	performRawExchangeRequest(t, router, "/v1/responses", []byte(`{"model":"gpt-test","stream":true}`), "")
	archive := loadSingleRawExchangeArchive(t, db)
	assert.Equal(t, model.RawExchangeOutcomeNonSuccess, archive.Outcome)
	assert.Equal(t, "incomplete_stream", archive.OutcomeReason)
	assert.False(t, archive.ResponseComplete)
}

func TestRawExchangeNonSuccessDiscardsNormallyEndedSSE(t *testing.T) {
	db, _ := setupRawExchangeMiddlewareTest(t)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureNonSuccess, "/v1/chat/completions", func(c *gin.Context) {
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
		service.AttachRawExchangeRelayInfo(c, &relaycommon.RelayInfo{IsStream: true, StreamStatus: status})
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: {\"choices\":[]}\n\n")
	})

	performRawExchangeRequest(t, router, "/v1/chat/completions", []byte(`{"model":"gpt-test","stream":true}`), "")
	var count int64
	require.NoError(t, db.Model(&model.RawExchangeArchive{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRawExchangeNonSuccessStoresTimedOutSSE(t *testing.T) {
	db, _ := setupRawExchangeMiddlewareTest(t)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureNonSuccess, "/v1/chat/completions", func(c *gin.Context) {
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(relaycommon.StreamEndReasonTimeout, assert.AnError)
		service.AttachRawExchangeRelayInfo(c, &relaycommon.RelayInfo{IsStream: true, StreamStatus: status})
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: {\"choices\":[]}\n\n")
	})

	performRawExchangeRequest(t, router, "/v1/chat/completions", []byte(`{"model":"gpt-test","stream":true}`), "")
	archive := loadSingleRawExchangeArchive(t, db)
	assert.Equal(t, model.RawExchangeOutcomeNonSuccess, archive.Outcome)
	assert.Equal(t, string(relaycommon.StreamEndReasonTimeout), archive.OutcomeReason)
	assert.False(t, archive.ResponseComplete)
}

func TestRawExchangeNonSuccessRejectsCompletedEventWithoutCompletedStatus(t *testing.T) {
	db, _ := setupRawExchangeMiddlewareTest(t)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureNonSuccess, "/v1/responses", func(c *gin.Context) {
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
		service.AttachRawExchangeRelayInfo(c, &relaycommon.RelayInfo{IsStream: true, StreamStatus: status})
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"failed\"}}\n\n")
	})

	performRawExchangeRequest(t, router, "/v1/responses", []byte(`{"model":"gpt-test","stream":true}`), "")
	archive := loadSingleRawExchangeArchive(t, db)
	assert.Equal(t, model.RawExchangeOutcomeNonSuccess, archive.Outcome)
	assert.Equal(t, "responses_failed", archive.OutcomeReason)
	assert.True(t, archive.ResponseComplete)
}

func TestRawExchangeNonSuccessDiscardsCompletedResponsesStream(t *testing.T) {
	db, _ := setupRawExchangeMiddlewareTest(t)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureNonSuccess, "/v1/responses", func(c *gin.Context) {
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
		service.AttachRawExchangeRelayInfo(c, &relaycommon.RelayInfo{IsStream: true, StreamStatus: status})
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	})

	performRawExchangeRequest(t, router, "/v1/responses", []byte(`{"model":"gpt-test","stream":true}`), "")
	var count int64
	require.NoError(t, db.Model(&model.RawExchangeArchive{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRawExchangeCaptureIncludesRecoveryResponse(t *testing.T) {
	db, root := setupRawExchangeMiddlewareTest(t)
	router := newRawExchangeTestRouter(model.RawExchangeCaptureNonSuccess, "/v1/chat/completions", func(c *gin.Context) {
		panic("relay panic")
	})

	recorder := performRawExchangeRequest(t, router, "/v1/chat/completions", []byte(`{"model":"gpt-test"}`), "")
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	archive := loadSingleRawExchangeArchive(t, db)
	assert.Equal(t, model.RawExchangeOutcomeNonSuccess, archive.Outcome)
	assert.JSONEq(t, `{"error":"relay panic"}`, string(readRawExchangePart(t, root, archive, rawExchangeResponseFileName)))
}
