package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingSessionAuthorizesGroupCreditAndStopsAtLimit(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)

	settings := ratio_setting.GetGroupRatioSetting().GroupCreditQuota
	original := settings.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, settings.UnmarshalJSON([]byte(original)))
	})
	require.NoError(t, settings.UnmarshalJSON([]byte(`{"vip":100}`)))

	const userID = 901
	seedUser(t, userID, 0)

	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			UserId:       userID,
			UserGroup:    "vip",
			IsPlayground: true,
		}
	}

	ctx, _ := gin.CreateTestContext(nil)
	firstInfo := newInfo()
	first, apiErr := NewBillingSession(ctx, firstInfo, 60)
	require.Nil(t, apiErr)
	require.NotNil(t, first)
	assert.Equal(t, 100, firstInfo.WalletCreditQuota)
	assert.Equal(t, 60, first.GetPreConsumedQuota())

	balance, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, -60, balance)

	// Authorized requests settle their full provider usage even if the actual
	// charge exceeds the estimate. The resulting debt blocks new requests.
	require.NoError(t, first.Settle(120))
	balance, err = model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, -120, balance)

	secondInfo := newInfo()
	second, apiErr := NewBillingSession(ctx, secondInfo, 1)
	require.Nil(t, second)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())

	balance, err = model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, -120, balance)
}

func TestCreditWalletDisablesTrustBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UserId:         1,
		UserQuota:      common.GetTrustQuota() * 2,
		TokenUnlimited: true,
	}
	session := &BillingSession{
		relayInfo: info,
		funding: &WalletFunding{
			userId:      1,
			creditQuota: 100,
		},
	}

	assert.False(t, session.shouldTrust(ctx))
}
