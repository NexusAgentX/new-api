package middleware

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TokenRequestCustomization applies token-level request rules after
// authentication and before rate limiting, channel selection, and billing.
func TokenRequestCustomization() gin.HandlerFunc {
	return func(c *gin.Context) {
		modelMapping, ok := common.GetContextKeyType[map[string]string](c, constant.ContextKeyTokenModelMapping)
		if !ok || len(modelMapping) == 0 {
			c.Next()
			return
		}

		modelRequest, shouldSelectChannel, err := getModelRequest(c)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		if shouldSelectChannel {
			if err := applyTokenRequestCustomization(c, modelRequest); err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
				return
			}
			common.SetContextKey(c, constant.ContextKeyRequestEffectiveModel, modelRequest.Model)
		}
		common.SetContextKey(c, constant.ContextKeyRequestCustomizationDone, true)
		c.Next()
	}
}

func applyTokenRequestCustomization(c *gin.Context, request *ModelRequest) error {
	if request == nil || request.Model == "" {
		return nil
	}
	modelMapping, ok := common.GetContextKeyType[map[string]string](c, constant.ContextKeyTokenModelMapping)
	if !ok || len(modelMapping) == 0 {
		return nil
	}

	requestModel := request.Model
	isResponsesCompact := strings.HasSuffix(requestModel, ratio_setting.CompactModelSuffix)
	if isResponsesCompact {
		requestModel = strings.TrimSuffix(requestModel, ratio_setting.CompactModelSuffix)
	}
	common.SetContextKey(c, constant.ContextKeyRequestModel, requestModel)
	effectiveModel, mapped, err := model.ResolveTokenModelMapping(modelMapping, requestModel)
	if err != nil {
		return err
	}
	if !mapped {
		return nil
	}
	if err := rewriteMappedRequestModel(c, requestModel, effectiveModel); err != nil {
		return err
	}

	request.Model = effectiveModel
	if isResponsesCompact {
		request.Model = ratio_setting.WithCompactModelSuffix(effectiveModel)
	}
	common.SetContextKey(c, constant.ContextKeyRequestModelMapped, true)
	return nil
}

func rewriteMappedRequestModel(c *gin.Context, requestModel string, effectiveModel string) error {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return nil
	}

	if path, changed := rewriteGeminiModelPath(c.Request.URL.Path, requestModel, effectiveModel); changed {
		c.Request.URL.Path = path
		c.Request.URL.RawPath = ""
	}
	for index := range c.Params {
		if c.Params[index].Key == "model" && c.Params[index].Value == requestModel {
			c.Params[index].Value = effectiveModel
		}
	}
	if strings.Contains(c.Request.URL.Path, "/engines/") {
		oldSegment := "/engines/" + requestModel + "/"
		newSegment := "/engines/" + effectiveModel + "/"
		c.Request.URL.Path = strings.Replace(c.Request.URL.Path, oldSegment, newSegment, 1)
		c.Request.URL.RawPath = ""
	}

	query := c.Request.URL.Query()
	if query.Get("model") == requestModel {
		query.Set("model", effectiveModel)
		c.Request.URL.RawQuery = query.Encode()
	}

	contentType := c.Request.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		return rewriteJSONRequestModel(c, requestModel, effectiveModel)
	case strings.HasPrefix(contentType, "application/x-www-form-urlencoded"):
		return rewriteFormRequestModel(c, requestModel, effectiveModel)
	case strings.HasPrefix(contentType, "multipart/form-data"):
		return rewriteMultipartRequestModel(c, contentType, requestModel, effectiveModel)
	default:
		return nil
	}
}

func rewriteGeminiModelPath(path string, requestModel string, effectiveModel string) (string, bool) {
	const modelPrefix = "/models/"
	prefixIndex := strings.Index(path, modelPrefix)
	if prefixIndex < 0 {
		return path, false
	}
	modelStart := prefixIndex + len(modelPrefix)
	modelEnd := len(path)
	if actionIndex := strings.Index(path[modelStart:], ":"); actionIndex >= 0 {
		modelEnd = modelStart + actionIndex
	}
	if path[modelStart:modelEnd] != requestModel {
		return path, false
	}
	return path[:modelStart] + effectiveModel + path[modelEnd:], true
}

func rewriteJSONRequestModel(c *gin.Context, requestModel string, effectiveModel string) error {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return err
	}
	body, err := storage.Bytes()
	if err != nil {
		return err
	}
	modelValue := gjson.GetBytes(body, "model")
	if !modelValue.Exists() || modelValue.Type != gjson.String || modelValue.String() != requestModel {
		return nil
	}
	updatedBody, err := sjson.SetBytes(body, "model", effectiveModel)
	if err != nil {
		return fmt.Errorf("failed to rewrite JSON request model: %w", err)
	}
	return common.ReplaceBodyStorage(c, updatedBody)
}

func rewriteFormRequestModel(c *gin.Context, requestModel string, effectiveModel string) error {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return err
	}
	body, err := storage.Bytes()
	if err != nil {
		return err
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return fmt.Errorf("failed to parse form request: %w", err)
	}
	if form.Get("model") != requestModel {
		return nil
	}
	form.Set("model", effectiveModel)
	return common.ReplaceBodyStorage(c, []byte(form.Encode()))
}

func rewriteMultipartRequestModel(c *gin.Context, contentType string, requestModel string, effectiveModel string) error {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("failed to parse multipart content type: %w", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return fmt.Errorf("multipart boundary is missing")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return err
	}
	body, err := storage.Bytes()
	if err != nil {
		return err
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var output bytes.Buffer
	writer := multipart.NewWriter(&output)
	if err := writer.SetBoundary(boundary); err != nil {
		return fmt.Errorf("failed to preserve multipart boundary: %w", err)
	}
	changed := false
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			return fmt.Errorf("failed to read multipart request: %w", partErr)
		}
		outputPart, createErr := writer.CreatePart(part.Header)
		if createErr != nil {
			_ = part.Close()
			return fmt.Errorf("failed to create multipart field: %w", createErr)
		}
		if part.FormName() == "model" {
			value, readErr := io.ReadAll(part)
			_ = part.Close()
			if readErr != nil {
				return fmt.Errorf("failed to read multipart model: %w", readErr)
			}
			if string(value) == requestModel {
				value = []byte(effectiveModel)
				changed = true
			}
			if _, writeErr := outputPart.Write(value); writeErr != nil {
				return fmt.Errorf("failed to write multipart model: %w", writeErr)
			}
			continue
		}
		if _, copyErr := io.Copy(outputPart, part); copyErr != nil {
			_ = part.Close()
			return fmt.Errorf("failed to copy multipart field: %w", copyErr)
		}
		_ = part.Close()
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to finish multipart request: %w", err)
	}
	if !changed {
		return nil
	}
	return common.ReplaceBodyStorage(c, output.Bytes())
}
