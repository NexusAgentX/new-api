package middleware

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRequestCustomizationContext(t *testing.T, target string, body io.Reader, contentType string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, target, body)
	if contentType != "" {
		ctx.Request.Header.Set("Content-Type", contentType)
	}
	t.Cleanup(func() {
		common.CleanupBodyStorage(ctx)
	})
	return ctx
}

func TestApplyTokenRequestCustomizationRewritesJSONBody(t *testing.T) {
	ctx := newRequestCustomizationContext(t, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.5","messages":[]}`), "application/json")
	common.SetContextKey(ctx, constant.ContextKeyTokenModelMapping, map[string]string{
		"gpt-5.5": "alias-2",
		"alias-2": "glm-5.2",
	})
	request := &ModelRequest{Model: "gpt-5.5"}

	require.NoError(t, applyTokenRequestCustomization(ctx, request))
	assert.Equal(t, "glm-5.2", request.Model)
	assert.Equal(t, "gpt-5.5", common.GetContextKeyString(ctx, constant.ContextKeyRequestModel))
	assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyRequestModelMapped))

	var rewritten ModelRequest
	require.NoError(t, common.UnmarshalBodyReusable(ctx, &rewritten))
	assert.Equal(t, "glm-5.2", rewritten.Model)
}

func TestTokenRequestCustomizationStoresDerivedEffectiveModel(t *testing.T) {
	ctx := newRequestCustomizationContext(t, "/suno/submit/generate", nil, "")
	ctx.Params = gin.Params{{Key: "action", Value: "generate"}}
	common.SetContextKey(ctx, constant.ContextKeyTokenModelMapping, map[string]string{"suno_generate": "suno-prod"})

	TokenRequestCustomization()(ctx)

	assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyRequestCustomizationDone))
	assert.Equal(t, "suno-prod", common.GetContextKeyString(ctx, constant.ContextKeyRequestEffectiveModel))
	assert.Equal(t, "suno_generate", common.GetContextKeyString(ctx, constant.ContextKeyRequestModel))
}

func TestApplyTokenRequestCustomizationPreservesResponsesCompactSemantics(t *testing.T) {
	ctx := newRequestCustomizationContext(t, "/v1/responses/compact", strings.NewReader(`{"model":"gpt-5.5","input":"test"}`), "application/json")
	common.SetContextKey(ctx, constant.ContextKeyTokenModelMapping, map[string]string{"gpt-5.5": "glm-5.2"})
	request := &ModelRequest{Model: ratio_setting.WithCompactModelSuffix("gpt-5.5")}

	require.NoError(t, applyTokenRequestCustomization(ctx, request))
	assert.Equal(t, ratio_setting.WithCompactModelSuffix("glm-5.2"), request.Model)
	assert.Equal(t, "gpt-5.5", common.GetContextKeyString(ctx, constant.ContextKeyRequestModel))

	var rewritten ModelRequest
	require.NoError(t, common.UnmarshalBodyReusable(ctx, &rewritten))
	assert.Equal(t, "glm-5.2", rewritten.Model)
}

func TestApplyTokenRequestCustomizationRecordsConfiguredMappingWithoutMatch(t *testing.T) {
	ctx := newRequestCustomizationContext(t, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4.1"}`), "application/json")
	common.SetContextKey(ctx, constant.ContextKeyTokenModelMapping, map[string]string{"gpt-5.5": "glm-5.2"})
	request := &ModelRequest{Model: "gpt-4.1"}

	require.NoError(t, applyTokenRequestCustomization(ctx, request))
	assert.Equal(t, "gpt-4.1", request.Model)
	assert.Equal(t, "gpt-4.1", common.GetContextKeyString(ctx, constant.ContextKeyRequestModel))
	assert.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyRequestModelMapped))
}

func TestApplyTokenRequestCustomizationRewritesURLEncodedForm(t *testing.T) {
	ctx := newRequestCustomizationContext(t, "/v1/audio/transcriptions", strings.NewReader("model=whisper-client&language=en"), "application/x-www-form-urlencoded")
	common.SetContextKey(ctx, constant.ContextKeyTokenModelMapping, map[string]string{"whisper-client": "whisper-1"})

	require.NoError(t, applyTokenRequestCustomization(ctx, &ModelRequest{Model: "whisper-client"}))
	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	body, err := storage.Bytes()
	require.NoError(t, err)
	form, err := url.ParseQuery(string(body))
	require.NoError(t, err)
	assert.Equal(t, "whisper-1", form.Get("model"))
	assert.Equal(t, "en", form.Get("language"))
}

func TestApplyTokenRequestCustomizationRewritesGeminiPathAndRealtimeQuery(t *testing.T) {
	geminiCtx := newRequestCustomizationContext(t, "/v1beta/models/gemini-client:generateContent", strings.NewReader(`{"contents":[]}`), "application/json")
	common.SetContextKey(geminiCtx, constant.ContextKeyTokenModelMapping, map[string]string{"gemini-client": "gemini-upstream"})
	require.NoError(t, applyTokenRequestCustomization(geminiCtx, &ModelRequest{Model: "gemini-client"}))
	assert.Equal(t, "/v1beta/models/gemini-upstream:generateContent", geminiCtx.Request.URL.Path)

	realtimeCtx := newRequestCustomizationContext(t, "/v1/realtime?model=realtime-client", nil, "")
	common.SetContextKey(realtimeCtx, constant.ContextKeyTokenModelMapping, map[string]string{"realtime-client": "realtime-upstream"})
	require.NoError(t, applyTokenRequestCustomization(realtimeCtx, &ModelRequest{Model: "realtime-client"}))
	assert.Equal(t, "realtime-upstream", realtimeCtx.Request.URL.Query().Get("model"))
}

func TestApplyTokenRequestCustomizationRewritesMultipartWithoutChangingFiles(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-client"))
	file, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	ctx := newRequestCustomizationContext(t, "/v1/images/edits", bytes.NewReader(body.Bytes()), writer.FormDataContentType())
	common.SetContextKey(ctx, constant.ContextKeyTokenModelMapping, map[string]string{"gpt-image-client": "gpt-image-1"})
	require.NoError(t, applyTokenRequestCustomization(ctx, &ModelRequest{Model: "gpt-image-client"}))

	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	rewrittenBody, err := storage.Bytes()
	require.NoError(t, err)
	reader := multipart.NewReader(bytes.NewReader(rewrittenBody), writer.Boundary())
	form, err := reader.ReadForm(int64(len(rewrittenBody)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })
	assert.Equal(t, []string{"gpt-image-1"}, form.Value["model"])
	require.Len(t, form.File["image"], 1)
	opened, err := form.File["image"][0].Open()
	require.NoError(t, err)
	fileBytes, err := io.ReadAll(opened)
	_ = opened.Close()
	require.NoError(t, err)
	assert.Equal(t, []byte("image-bytes"), fileBytes)
}
