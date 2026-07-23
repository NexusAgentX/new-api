package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetUserRankingsRejectsInvalidParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []string{
		"/api/user-rankings?period=year",
		"/api/user-rankings?sort=cost",
		"/api/user-rankings?limit=0",
		"/api/user-rankings?limit=51",
		"/api/user-rankings?limit=bad",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Set("role", common.RoleCommonUser)
			ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)

			GetUserRankings(ctx)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestGetUserRankingsReportsDisabledDataExport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := common.DataExportEnabled
	common.DataExportEnabled = false
	t.Cleanup(func() {
		common.DataExportEnabled = previous
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleCommonUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user-rankings", nil)

	GetUserRankings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			DataAvailable     bool   `json:"data_available"`
			UnavailableReason string `json:"unavailable_reason"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.False(t, payload.Data.DataAvailable)
	require.Equal(t, "data_export_disabled", payload.Data.UnavailableReason)
}
