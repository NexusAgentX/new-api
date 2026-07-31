package controller

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxRawExchangeBatchDelete                 = 100
	securityProofScopeRawExchangeRead         = "raw_exchange.read"
	securityProofScopeRawExchangeDelete       = "raw_exchange.delete"
	securityProofScopeRawExchangeAdminCleanup = "raw_exchange.admin.cleanup"
)

type rawExchangeBatchDeleteRequest struct {
	RequestIds []string `json:"request_ids"`
}

type rawExchangeCleanupPreviewRequest struct {
	Filter model.RawExchangeCleanupFilter `json:"filter"`
}

type rawExchangeCleanupStartRequest struct {
	PreviewToken string `json:"preview_token"`
	Confirmation string `json:"confirmation"`
}

func GetRawExchangeAdminStats(c *gin.Context) {
	stats, err := model.GetRawExchangeAdminStats(10)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	latestGC, err := model.GetLatestSystemTask(model.SystemTaskTypeRawExchangeGC)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var latestGCResponse any
	if latestGC != nil {
		latestGCResponse = latestGC.ToResponse()
	}
	readyErr := service.RawExchangeStorageReady()
	settings := *system_setting.GetRawExchangeSettings()
	common.ApiSuccess(c, gin.H{
		"stats":              stats,
		"capture_enabled":    settings.Enabled,
		"storage_ready":      readyErr == nil,
		"storage_error":      rawExchangeStorageError(readyErr),
		"storage_backend":    service.RawExchangeStorageBackend(),
		"retention_days":     settings.RetentionDays,
		"capacity_bytes":     settings.GlobalCapacityBytes,
		"max_request_bytes":  settings.MaxRequestBytes,
		"max_response_bytes": settings.MaxResponseBytes,
		"max_exchange_bytes": settings.MaxExchangeBytes,
		"latest_gc":          latestGCResponse,
	})
}

func PreviewRawExchangeCleanup(c *gin.Context) {
	var request rawExchangeCleanupPreviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid cleanup filter"})
		return
	}
	preview, token, expiresAt, err := service.CreateRawExchangeCleanupPreview(request.Filter, c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	recordManageAudit(c, "raw_exchange.cleanup_preview", map[string]interface{}{
		"filter":         request.Filter,
		"count":          preview.Count,
		"request_bytes":  preview.RequestStoredBytes,
		"response_bytes": preview.ResponseStoredBytes,
		"total_bytes":    preview.TotalStoredBytes,
		"expires_at":     expiresAt,
	})
	common.ApiSuccess(c, gin.H{
		"preview":       preview,
		"preview_token": token,
		"expires_at":    expiresAt,
	})
}

func CreateRawExchangeCleanupSystemTask(c *gin.Context) {
	if !middleware.RequireSecurityProof(c, securityProofScopeRawExchangeAdminCleanup, []string{secureVerificationMethod2FA, secureVerificationMethodPasskey}) {
		return
	}
	var request rawExchangeCleanupStartRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.PreviewToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "preview_token is required"})
		return
	}
	filter, preview, err := service.VerifyRawExchangeCleanupPreview(request.PreviewToken, c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if rawExchangeCleanupMatchesAll(filter) && request.Confirmation != "DELETE ALL RAW EXCHANGES" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "full cleanup confirmation is required"})
		return
	}
	task, created, err := service.StartRawExchangeCleanupTask(filter, service.RawExchangeCleanupAuditContext{
		OperatorId:       c.GetInt("id"),
		OperatorUsername: c.GetString("username"),
		OperatorRole:     c.GetInt("role"),
		AuthMethod:       auditAuthMethod(c),
		Ip:               c.ClientIP(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !created {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "raw exchange cleanup is already in progress"})
		return
	}
	recordManageAudit(c, "raw_exchange.cleanup_start", map[string]interface{}{
		"filter":         filter,
		"task_id":        task.TaskID,
		"created":        created,
		"count":          preview.Count,
		"request_bytes":  preview.RequestStoredBytes,
		"response_bytes": preview.ResponseStoredBytes,
		"total_bytes":    preview.TotalStoredBytes,
	})
	common.ApiSuccess(c, gin.H{"task": task.ToResponse(), "created": created})
}

func DownloadRawExchangeRequest(c *gin.Context) {
	if requireRawExchangeSecurityProof(c, securityProofScopeRawExchangeRead) {
		downloadRawExchangePart(c, true)
	}
}

func DownloadRawExchangeResponse(c *gin.Context) {
	if requireRawExchangeSecurityProof(c, securityProofScopeRawExchangeRead) {
		downloadRawExchangePart(c, false)
	}
}

func DownloadRawExchangeBundle(c *gin.Context) {
	if !requireRawExchangeSecurityProof(c, securityProofScopeRawExchangeRead) {
		return
	}
	download, err := service.AcquireRawExchangeDownload(c.Param("request_id"), c.GetInt("id"))
	if err != nil {
		writeRawExchangeAccessError(c, err)
		return
	}
	defer func() { _ = download.Close() }()

	requestReader, _, err := download.OpenRequest()
	if err != nil {
		writeRawExchangeAccessError(c, err)
		return
	}
	defer requestReader.Close()

	var responseReader io.ReadCloser
	if download.Archive().ResponseAvailable {
		responseReader, _, err = download.OpenResponse()
		if err != nil {
			writeRawExchangeAccessError(c, err)
			return
		}
		defer responseReader.Close()
	}

	setRawExchangeDownloadHeaders(c, rawExchangeFilename(download.Archive().RequestId, "exchange.zip"))
	c.Header("Content-Type", "application/zip")
	c.Status(http.StatusOK)
	zipWriter := zip.NewWriter(c.Writer)
	if err := writeRawExchangeZipPart(zipWriter, "request.body", requestReader); err != nil {
		logger.LogWarn(c, "raw exchange bundle request write failed: "+err.Error())
		_ = zipWriter.Close()
		return
	}
	if responseReader != nil {
		if err := writeRawExchangeZipPart(zipWriter, "response.body", responseReader); err != nil {
			logger.LogWarn(c, "raw exchange bundle response write failed: "+err.Error())
			_ = zipWriter.Close()
			return
		}
	}
	metadata, err := common.Marshal(download.Archive().DownloadMetadata())
	if err != nil {
		logger.LogWarn(c, "raw exchange bundle metadata marshal failed: "+err.Error())
		_ = zipWriter.Close()
		return
	}
	if err := writeRawExchangeZipPart(zipWriter, "metadata.json", strings.NewReader(string(metadata))); err != nil {
		logger.LogWarn(c, "raw exchange bundle metadata write failed: "+err.Error())
		_ = zipWriter.Close()
		return
	}
	if err := zipWriter.Close(); err != nil {
		logger.LogWarn(c, "raw exchange bundle close failed: "+err.Error())
	}
}

func DeleteRawExchange(c *gin.Context) {
	if !requireRawExchangeSecurityProof(c, securityProofScopeRawExchangeDelete) {
		return
	}
	alreadyDeleted, requestBytes, responseBytes, err := service.DeleteRawExchangeForUser(c.Param("request_id"), c.GetInt("id"))
	if errors.Is(err, service.ErrRawExchangeDeletionDeferred) {
		c.JSON(http.StatusAccepted, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"deleted":              false,
				"deletion_pending":     true,
				"request_bytes_freed":  0,
				"response_bytes_freed": 0,
				"total_bytes_freed":    0,
			},
		})
		return
	}
	if err != nil {
		writeRawExchangeAccessError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"deleted":              !alreadyDeleted,
		"already_deleted":      alreadyDeleted,
		"request_bytes_freed":  requestBytes,
		"response_bytes_freed": responseBytes,
		"total_bytes_freed":    requestBytes + responseBytes,
	})
}

func DeleteRawExchangeBatch(c *gin.Context) {
	if !requireRawExchangeSecurityProof(c, securityProofScopeRawExchangeDelete) {
		return
	}
	var request rawExchangeBatchDeleteRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(request.RequestIds) == 0 || len(request.RequestIds) > maxRawExchangeBatchDelete {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "request_ids must contain between 1 and 100 entries"})
		return
	}
	common.ApiSuccess(c, service.DeleteRawExchangeBatchForUser(request.RequestIds, c.GetInt("id")))
}

func requireRawExchangeSecurityProof(c *gin.Context, scope string) bool {
	return middleware.RequireSecurityProof(c, scope, []string{secureVerificationMethod2FA, secureVerificationMethodPasskey})
}

func downloadRawExchangePart(c *gin.Context, requestPart bool) {
	download, err := service.AcquireRawExchangeDownload(c.Param("request_id"), c.GetInt("id"))
	if err != nil {
		writeRawExchangeAccessError(c, err)
		return
	}
	defer func() { _ = download.Close() }()

	var (
		reader      io.ReadCloser
		size        int64
		contentType string
		suffix      string
	)
	if requestPart {
		reader, size, err = download.OpenRequest()
		contentType = download.Archive().RequestContentType
		suffix = "request.body"
	} else {
		reader, size, err = download.OpenResponse()
		contentType = download.Archive().ResponseContentType
		suffix = "response.body"
	}
	if err != nil {
		writeRawExchangeAccessError(c, err)
		return
	}
	defer reader.Close()
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	setRawExchangeDownloadHeaders(c, rawExchangeFilename(download.Archive().RequestId, suffix))
	c.DataFromReader(http.StatusOK, size, contentType, reader, nil)
}

func writeRawExchangeZipPart(writer *zip.Writer, name string, reader io.Reader) error {
	part, err := writer.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, reader)
	return err
}

func setRawExchangeDownloadHeaders(c *gin.Context, filename string) {
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store, private, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
}

func rawExchangeFilename(requestId, suffix string) string {
	var clean strings.Builder
	for _, char := range requestId {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			clean.WriteRune(char)
		}
		if clean.Len() >= 16 {
			break
		}
	}
	if clean.Len() == 0 {
		clean.WriteString("exchange")
	}
	return clean.String() + "-" + suffix
}

func rawExchangeCleanupMatchesAll(filter model.RawExchangeCleanupFilter) bool {
	if filter.AfterTimestamp != 0 || filter.BeforeTimestamp != 0 || filter.UserId != 0 || filter.TokenId != 0 ||
		filter.CaptureMode != "" || filter.Outcome != "" || filter.Status != "" {
		return false
	}
	if len(filter.Statuses) == 0 {
		return true
	}
	remaining := map[string]struct{}{
		model.RawExchangeStatusPendingCommit:   {},
		model.RawExchangeStatusStored:          {},
		model.RawExchangeStatusSkippedTooLarge: {},
		model.RawExchangeStatusSkippedCapacity: {},
		model.RawExchangeStatusCaptureFailed:   {},
		model.RawExchangeStatusDeletePending:   {},
		model.RawExchangeStatusDeleteFailed:    {},
		model.RawExchangeStatusDeleted:         {},
		model.RawExchangeStatusMissing:         {},
	}
	for _, status := range filter.Statuses {
		delete(remaining, status)
	}
	return len(remaining) == 0
}

func rawExchangeStorageError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func writeRawExchangeAccessError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, model.ErrRawExchangeNotStored):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "raw exchange archive not found"})
	case errors.Is(err, model.ErrRawExchangeDownloadInProgress), errors.Is(err, model.ErrRawExchangeDeletionInProgress):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
	default:
		logger.LogWarn(c, "raw exchange archive access failed: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to access raw exchange archive"})
	}
}
