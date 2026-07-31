package middleware

import (
	"mime"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func RawExchangeLifecycle() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer service.FinalizeRawExchangeCapture(c)
		c.Next()
	}
}

func RawExchangeCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		mode := common.GetContextKeyString(c, constant.ContextKeyTokenRawExchangeCaptureMode)
		if mode == model.RawExchangeCaptureOff || !rawExchangeCaptureSupported(c) {
			c.Next()
			return
		}
		if session := service.StartRawExchangeCapture(c, mode); session != nil {
			c.Writer = &rawExchangeResponseWriter{ResponseWriter: c.Writer, context: c}
		}
		c.Next()
	}
}

type rawExchangeResponseWriter struct {
	gin.ResponseWriter
	context *gin.Context
}

func (w *rawExchangeResponseWriter) Write(data []byte) (int, error) {
	accepted, err := w.ResponseWriter.Write(data)
	service.CaptureRawExchangeResponse(w.context, data, accepted, err)
	return accepted, err
}

func (w *rawExchangeResponseWriter) WriteString(data string) (int, error) {
	accepted, err := w.ResponseWriter.WriteString(data)
	service.CaptureRawExchangeResponse(w.context, []byte(data), accepted, err)
	return accepted, err
}

func (w *rawExchangeResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func rawExchangeCaptureSupported(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Method != "POST" {
		return false
	}
	contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || (!strings.HasPrefix(mediaType, "text/") && mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json")) {
			return false
		}
	}

	path := c.Request.URL.Path
	switch path {
	case "/v1/messages",
		"/v1/completions",
		"/v1/chat/completions",
		"/v1/responses",
		"/v1/responses/compact",
		"/v1/alpha/search",
		"/v1/embeddings",
		"/v1/rerank",
		"/v1/moderations":
		return true
	}
	if strings.HasPrefix(path, "/v1/engines/") && strings.HasSuffix(path, "/embeddings") {
		return true
	}
	if strings.HasPrefix(path, "/v1/models/") || strings.HasPrefix(path, "/v1beta/models/") {
		for _, action := range []string{":generateContent", ":streamGenerateContent", ":embedContent", ":batchEmbedContents"} {
			if strings.HasSuffix(path, action) {
				return true
			}
		}
	}
	return false
}
