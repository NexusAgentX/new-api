package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoRecordsTokenModelMappingSeparatelyFromChannelMapping(t *testing.T) {
	now := time.Now()
	relayInfo := &common.RelayInfo{
		OriginModelName:                "adapter-mutated-model",
		GatewayModelName:               "glm-5.2",
		RequestModelName:               "gpt-5.5",
		RequestCustomizationConfigured: true,
		RequestModelMapped:             true,
		StartTime:                      now,
		FirstResponseTime:              now,
		ChannelMeta: &common.ChannelMeta{
			IsModelMapped:     true,
			UpstreamModelName: "vendor/glm-prod",
		},
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 1, 0, 1)

	requestCustomization, ok := other["request_customization"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, requestCustomization["configured"])
	modelMapping, ok := requestCustomization["model_mapping"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, modelMapping["applied"])
	assert.Equal(t, "gpt-5.5", modelMapping["original_model"])
	assert.Equal(t, "glm-5.2", modelMapping["effective_model"])
	assert.Equal(t, true, other["is_model_mapped"])
	assert.Equal(t, "vendor/glm-prod", other["upstream_model_name"])
}

func TestGenerateTextOtherInfoRecordsConfiguredMappingThatWasNotApplied(t *testing.T) {
	now := time.Now()
	relayInfo := &common.RelayInfo{
		OriginModelName:                "gpt-4.1",
		GatewayModelName:               "gpt-4.1",
		RequestModelName:               "gpt-4.1",
		RequestCustomizationConfigured: true,
		StartTime:                      now,
		FirstResponseTime:              now,
		ChannelMeta:                    &common.ChannelMeta{},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 1, 0, 1)

	requestCustomization, ok := other["request_customization"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, requestCustomization["configured"])
	modelMapping, ok := requestCustomization["model_mapping"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, modelMapping["applied"])
	assert.Equal(t, "gpt-4.1", modelMapping["original_model"])
	assert.Equal(t, "gpt-4.1", modelMapping["effective_model"])
}

func TestTaskBillingOtherPreservesRequestAndChannelModelMappings(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{
			RequestCustomizationConfigured: true,
			RequestModelName:               "gpt-5.5",
			OriginModelName:                "glm-5.2",
			UpstreamModelName:              "vendor/glm-prod",
		},
	}

	other := taskBillingOther(task)

	requestCustomization, ok := other["request_customization"].(map[string]interface{})
	require.True(t, ok)
	modelMapping, ok := requestCustomization["model_mapping"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, modelMapping["applied"])
	assert.Equal(t, "gpt-5.5", modelMapping["original_model"])
	assert.Equal(t, "glm-5.2", modelMapping["effective_model"])
	assert.Equal(t, true, other["is_model_mapped"])
	assert.Equal(t, "vendor/glm-prod", other["upstream_model_name"])
}
