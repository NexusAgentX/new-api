package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetRankings(c *gin.Context) {
	result, err := service.GetRankingsSnapshot(c.DefaultQuery("period", "week"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetUserRankings(c *gin.Context) {
	limit := 20
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "invalid user ranking limit",
			})
			return
		}
		limit = parsedLimit
	}

	result, err := service.GetUserRankings(
		c.DefaultQuery("period", "today"),
		c.DefaultQuery("sort", "tokens"),
		limit,
		c.GetInt("role") >= common.RoleAdminUser,
	)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrInvalidUserRankingPeriod) ||
			errors.Is(err, service.ErrInvalidUserRankingSort) ||
			errors.Is(err, service.ErrInvalidUserRankingLimit) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
