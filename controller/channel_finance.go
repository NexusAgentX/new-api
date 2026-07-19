package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const maxChannelFinanceRange = 366 * 24 * time.Hour

func GetChannelFinance(c *gin.Context) {
	startTimestamp, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || startTimestamp < 0 {
		common.ApiErrorMsg(c, "invalid start timestamp")
		return
	}
	endTimestamp, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || endTimestamp < startTimestamp {
		common.ApiErrorMsg(c, "invalid end timestamp")
		return
	}
	if endTimestamp-startTimestamp > int64(maxChannelFinanceRange/time.Second) {
		common.ApiErrorMsg(c, "channel finance range cannot exceed 366 days")
		return
	}

	granularity := c.DefaultQuery("granularity", model.ChannelFinanceGranularityDay)
	timezone := c.DefaultQuery("timezone", "UTC")
	if len(timezone) > 64 {
		common.ApiErrorMsg(c, "invalid timezone")
		return
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid timezone: %w", err))
		return
	}

	report, err := model.GetChannelFinanceReport(startTimestamp, endTimestamp, granularity, location)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    report,
	})
}
