package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckGroupCreditQuota(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid", value: `{"default":0,"vip":500000}`},
		{name: "negative", value: `{"vip":-1}`, wantErr: true},
		{name: "fractional", value: `{"vip":1.5}`, wantErr: true},
		{name: "overflow", value: `{"vip":2147483648}`, wantErr: true},
		{name: "not object", value: `[]`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckGroupCreditQuota(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGetGroupAvailableQuotaIncludesCredit(t *testing.T) {
	settings := GetGroupRatioSetting().GroupCreditQuota
	original := settings.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, settings.UnmarshalJSON([]byte(original)))
	})
	require.NoError(t, settings.UnmarshalJSON([]byte(`{"vip":100}`)))

	assert.Equal(t, 100, GetGroupCreditQuota("vip"))
	assert.Equal(t, int64(40), GetGroupAvailableQuota("vip", -60))
	assert.Zero(t, GetGroupCreditQuota("default"))
}
