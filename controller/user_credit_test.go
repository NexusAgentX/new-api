package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAllUsersIncludesGroupCreditQuota(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	creditSettings := ratio_setting.GetGroupRatioSetting().GroupCreditQuota
	original := creditSettings.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, creditSettings.UnmarshalJSON([]byte(original)))
	})
	require.NoError(t, creditSettings.UnmarshalJSON([]byte(`{"vip":100}`)))

	require.NoError(t, db.Create(&model.User{
		Username: "credit-user",
		AffCode:  "credit-code",
		Group:    "vip",
		Quota:    -40,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Username: "cash-user",
		AffCode:  "cash-code",
		Group:    "default",
		Quota:    25,
		Status:   common.UserStatusEnabled,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/?p=1&page_size=10", nil)
	GetAllUsers(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []model.User `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 2)

	users := make(map[string]model.User, len(response.Data.Items))
	for _, user := range response.Data.Items {
		users[user.Username] = user
	}

	assert.Equal(t, 100, users["credit-user"].CreditQuota)
	assert.Equal(t, int64(60), users["credit-user"].AvailableQuota)
	assert.Zero(t, users["cash-user"].CreditQuota)
	assert.Equal(t, int64(25), users["cash-user"].AvailableQuota)
}
