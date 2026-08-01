package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	channelmetrics "github.com/QuantumNous/new-api/pkg/channel_metrics"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedChannelMetricsControllerTest(t *testing.T) *model.Channel {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := &model.Channel{
		Id:     99201,
		Name:   "observed-channel",
		Type:   1,
		Key:    "super-secret-channel-key",
		Models: "gpt-test",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	channel.SetSetting(dto.ChannelSettings{MaxConcurrency: 5, RPMLimit: 30})
	require.NoError(t, db.Create(channel).Error)
	now := time.Now()
	require.NoError(t, model.UpsertChannelMetric(&model.ChannelMetric{
		ChannelId:             channel.Id,
		Group:                 "default",
		ModelName:             "gpt-test",
		Endpoint:              "/v1/responses",
		BucketTs:              now.Truncate(time.Minute).Unix(),
		BucketSeconds:         model.ChannelMetricBucketMinute,
		HistogramVersion:      channelmetrics.HistogramVersion,
		AttemptCount:          2,
		SuccessCount:          1,
		Upstream429Count:      1,
		ErrorCode:             "rate_limit_exceeded",
		StatusCode:            http.StatusTooManyRequests,
		OutputTokens:          120,
		GenerationMs:          2000,
		ThroughputSampleCount: 1,
		PeakConcurrency:       3,
		TtftLe1000Ms:          2,
	}))
	require.NoError(t, db.Create(&model.ChannelStatusEvent{
		ChannelId:            channel.Id,
		Scope:                "channel",
		FromStatus:           common.ChannelStatusAutoDisabled,
		ToStatus:             common.ChannelStatusEnabled,
		ChannelStatusBefore:  common.ChannelStatusAutoDisabled,
		ChannelStatusAfter:   common.ChannelStatusEnabled,
		Source:               "scheduled_channel_test",
		ReasonCode:           "channel_test_succeeded",
		ReasonDetail:         "Scheduled recovery succeeded",
		PreviousReasonCode:   "upstream_http_429",
		PreviousReasonDetail: "Upstream attempt failed with HTTP 429",
		CreatedAt:            now.Unix(),
	}).Error)
	return channel
}

func performChannelMetricControllerRequest(t *testing.T, method string, target string, pathId string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, nil)
	if pathId != "" {
		c.Params = gin.Params{{Key: "id", Value: pathId}}
	}
	handler(c)
	return recorder
}

func TestGetChannelMetricsOverviewAndDetailExposeOnlySafeAggregates(t *testing.T) {
	channel := seedChannelMetricsControllerTest(t)

	overviewRecorder := performChannelMetricControllerRequest(
		t,
		http.MethodGet,
		"/api/channel/metrics/overview?hours=1&channel_id=99201&group=default&model=gpt-test&endpoint=/v1/responses",
		"",
		GetChannelMetricsOverview,
	)
	require.Equal(t, http.StatusOK, overviewRecorder.Code)
	assert.NotContains(t, overviewRecorder.Body.String(), channel.Key)
	var overviewResponse struct {
		Success bool                          `json:"success"`
		Data    channelmetrics.OverviewResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(overviewRecorder.Body.Bytes(), &overviewResponse))
	require.True(t, overviewResponse.Success)
	assert.Equal(t, 7, overviewResponse.Data.MinuteRetentionDays)
	assert.Equal(t, 90, overviewResponse.Data.HourRetentionDays)
	require.Len(t, overviewResponse.Data.Items, 1)
	item := overviewResponse.Data.Items[0]
	assert.Equal(t, channel.Id, item.ChannelId)
	assert.Equal(t, int64(2), item.Metrics.AttemptCount)
	assert.Equal(t, 50.0, item.Metrics.AvailabilityRate)
	assert.Equal(t, 60.0, *item.Metrics.AverageTps)
	require.NotNil(t, item.LatestStatusEvent)
	assert.Equal(t, "upstream_http_429", item.LatestStatusEvent.PreviousReasonCode)

	dimensionsRecorder := performChannelMetricControllerRequest(
		t,
		http.MethodGet,
		"/api/channel/metrics/dimensions?hours=1&channel_id=99201",
		"",
		GetChannelMetricDimensions,
	)
	require.Equal(t, http.StatusOK, dimensionsRecorder.Code)
	var dimensionsResponse struct {
		Data channelmetrics.DimensionResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(dimensionsRecorder.Body.Bytes(), &dimensionsResponse))
	assert.Equal(t, []string{"default"}, dimensionsResponse.Data.Groups)
	assert.Equal(t, []string{"gpt-test"}, dimensionsResponse.Data.Models)
	assert.Equal(t, []string{"/v1/responses"}, dimensionsResponse.Data.Endpoints)

	detailRecorder := performChannelMetricControllerRequest(
		t,
		http.MethodGet,
		"/api/channel/metrics/99201?hours=1",
		"99201",
		GetChannelMetricsDetail,
	)
	require.Equal(t, http.StatusOK, detailRecorder.Code)
	assert.NotContains(t, detailRecorder.Body.String(), channel.Key)
	var detailResponse struct {
		Success bool                        `json:"success"`
		Data    channelmetrics.DetailResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(detailRecorder.Body.Bytes(), &detailResponse))
	require.True(t, detailResponse.Success)
	assert.Equal(t, 7, detailResponse.Data.MinuteRetentionDays)
	assert.Equal(t, 90, detailResponse.Data.HourRetentionDays)
	assert.Equal(t, channel.Name, detailResponse.Data.ChannelName)
	require.Len(t, detailResponse.Data.Series, 1)
	assert.Equal(t, 2.0, detailResponse.Data.Series[0].Metrics.RPM)
	require.Len(t, detailResponse.Data.ErrorBreakdown, 1)
	assert.Equal(t, http.StatusTooManyRequests, detailResponse.Data.ErrorBreakdown[0].HTTPStatus)
	assert.Equal(t, "rate_limit_exceeded", detailResponse.Data.ErrorBreakdown[0].ErrorCode)
	assert.Equal(t, int64(1), detailResponse.Data.ErrorBreakdown[0].Count)
	require.Len(t, detailResponse.Data.StatusEvents, 1)
}

func TestGetChannelMetricsRuntimeReturnsLimitsAndLiveMode(t *testing.T) {
	channel := seedChannelMetricsControllerTest(t)
	recorder := performChannelMetricControllerRequest(
		t,
		http.MethodGet,
		"/api/channel/metrics/runtime?channel_id=99201",
		"",
		GetChannelMetricsRuntime,
	)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), channel.Key)
	var response struct {
		Success bool                         `json:"success"`
		Data    channelmetrics.RuntimeResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, 5, response.Data.Items[0].MaxConcurrency)
	assert.Equal(t, 30, response.Data.Items[0].RPMLimit)
	assert.Equal(t, channelmetrics.RuntimeModeMemory, response.Data.Items[0].Runtime.Mode)
}

func TestGetChannelMetricsRuntimeDoesNotMutateChannelWithInvalidSettings(t *testing.T) {
	channel := seedChannelMetricsControllerTest(t)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("setting", "{").Error)

	recorder := performChannelMetricControllerRequest(
		t,
		http.MethodGet,
		"/api/channel/metrics/runtime?channel_id=99201",
		"",
		GetChannelMetricsRuntime,
	)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data channelmetrics.RuntimeResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data.Items, 1)
	assert.Zero(t, response.Data.Items[0].MaxConcurrency)
	assert.Zero(t, response.Data.Items[0].RPMLimit)

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, channel.Key, stored.Key)
	assert.Equal(t, channel.Name, stored.Name)
	assert.Equal(t, channel.Status, stored.Status)
	assert.Equal(t, "{", *stored.Setting)
}

func TestChannelMetricControllersRejectInvalidQueriesAndMissingChannels(t *testing.T) {
	seedChannelMetricsControllerTest(t)
	tests := []struct {
		name    string
		target  string
		pathId  string
		handler gin.HandlerFunc
		status  int
	}{
		{name: "invalid hours", target: "/api/channel/metrics/overview?hours=0", handler: GetChannelMetricsOverview, status: http.StatusBadRequest},
		{name: "reversed range", target: "/api/channel/metrics/overview?start_timestamp=200&end_timestamp=100", handler: GetChannelMetricsOverview, status: http.StatusBadRequest},
		{name: "invalid runtime channel", target: "/api/channel/metrics/runtime?channel_id=-1", handler: GetChannelMetricsRuntime, status: http.StatusBadRequest},
		{name: "invalid detail channel", target: "/api/channel/metrics/nope", pathId: "nope", handler: GetChannelMetricsDetail, status: http.StatusBadRequest},
		{name: "missing detail channel", target: "/api/channel/metrics/99999", pathId: "99999", handler: GetChannelMetricsDetail, status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performChannelMetricControllerRequest(t, http.MethodGet, test.target, test.pathId, test.handler)
			assert.Equal(t, test.status, recorder.Code)
		})
	}
}
