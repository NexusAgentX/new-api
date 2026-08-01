package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	channelmetrics "github.com/QuantumNous/new-api/pkg/channel_metrics"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetChannelMetricsOverview(c *gin.Context) {
	params, err := parseChannelMetricQuery(c, true)
	if err != nil {
		channelMetricQueryError(c, err)
		return
	}
	result, err := channelmetrics.QueryOverview(c.Request.Context(), params)
	if err != nil {
		channelMetricInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func GetChannelMetricDimensions(c *gin.Context) {
	params, err := parseChannelMetricQuery(c, true)
	if err != nil {
		channelMetricQueryError(c, err)
		return
	}
	result, err := channelmetrics.QueryDimensions(params)
	if err != nil {
		channelMetricInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func GetChannelMetricsDetail(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		channelMetricQueryError(c, errors.New("channel id must be a positive integer"))
		return
	}
	params, err := parseChannelMetricQuery(c, false)
	if err != nil {
		channelMetricQueryError(c, err)
		return
	}
	params.ChannelId = channelId
	result, err := channelmetrics.QueryDetail(c.Request.Context(), params)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "channel not found"})
		return
	}
	if err != nil {
		channelMetricInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func GetChannelMetricsRuntime(c *gin.Context) {
	channelId := 0
	if raw := strings.TrimSpace(c.Query("channel_id")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			channelMetricQueryError(c, errors.New("channel_id must be a positive integer"))
			return
		}
		channelId = parsed
	}
	result, err := channelmetrics.QueryRuntime(c.Request.Context(), channelId)
	if err != nil {
		channelMetricInternalError(c, err)
		return
	}
	if channelId > 0 && len(result.Items) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "channel not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func parseChannelMetricQuery(c *gin.Context, allowChannelId bool) (channelmetrics.QueryParams, error) {
	params := channelmetrics.QueryParams{
		Group:     strings.TrimSpace(c.Query("group")),
		ModelName: strings.TrimSpace(c.Query("model")),
		Endpoint:  strings.TrimSpace(c.Query("endpoint")),
		Hours:     24,
	}
	if len([]rune(params.Group)) > 64 {
		return params, errors.New("group must not exceed 64 characters")
	}
	if len([]rune(params.ModelName)) > 128 {
		return params, errors.New("model must not exceed 128 characters")
	}
	if len([]rune(params.Endpoint)) > 128 {
		return params, errors.New("endpoint must not exceed 128 characters")
	}

	maxHours := int(channelmetrics.HourRetention / time.Hour)
	if raw := strings.TrimSpace(c.Query("hours")); raw != "" {
		hours, err := strconv.Atoi(raw)
		if err != nil || hours < 1 || hours > maxHours {
			return params, fmt.Errorf("hours must be between 1 and %d", maxHours)
		}
		params.Hours = hours
	}
	var err error
	params.StartTs, err = parseChannelMetricTimestamp(c.Query("start_timestamp"), "start_timestamp")
	if err != nil {
		return params, err
	}
	params.EndTs, err = parseChannelMetricTimestamp(c.Query("end_timestamp"), "end_timestamp")
	if err != nil {
		return params, err
	}

	now := time.Now().Unix()
	effectiveEnd := params.EndTs
	if effectiveEnd == 0 {
		effectiveEnd = now
	}
	if effectiveEnd > now+60 {
		return params, errors.New("end_timestamp must not be in the future")
	}
	if params.StartTs > 0 {
		if params.StartTs > effectiveEnd {
			return params, errors.New("start_timestamp must not be after end_timestamp")
		}
		if effectiveEnd-params.StartTs > int64(channelmetrics.HourRetention/time.Second) {
			return params, fmt.Errorf("time range must not exceed %d days", int(channelmetrics.HourRetention/(24*time.Hour)))
		}
	}

	if allowChannelId {
		if raw := strings.TrimSpace(c.Query("channel_id")); raw != "" {
			channelId, err := strconv.Atoi(raw)
			if err != nil || channelId <= 0 {
				return params, errors.New("channel_id must be a positive integer")
			}
			params.ChannelId = channelId
		}
	}
	return params, nil
}

func parseChannelMetricTimestamp(raw string, name string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive Unix timestamp", name)
	}
	return value, nil
}

func channelMetricQueryError(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
}

func channelMetricInternalError(c *gin.Context, err error) {
	common.SysError("query channel metrics failed: " + err.Error())
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to query channel metrics"})
}
