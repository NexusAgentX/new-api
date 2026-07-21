package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterAutoGroupsByTokenPolicyPreservesBaseOrderAndPermissions(t *testing.T) {
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalSpecialRatios := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalSpecialRatios))
	})

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["cheap","preferred","premium"]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"cheap":"","preferred":"","premium":""}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"cheap":0.5,"preferred":1,"premium":3}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"preferred":2}}`))

	maxDefault := 1.5
	policy := &model.TokenAutoGroupPolicy{
		DefaultRule: &model.TokenAutoGroupRule{
			Mode:     model.TokenAutoGroupModeDenylist,
			Groups:   []string{"premium"},
			MaxRatio: &maxDefault,
		},
		ModelRules: map[string]model.TokenAutoGroupRule{
			"gpt-special": {
				Mode:   model.TokenAutoGroupModeAllowlist,
				Groups: []string{"preferred", "premium"},
			},
		},
	}

	baseGroups := []string{"preferred", "cheap", "premium"}
	assert.Equal(t, []string{"cheap"}, FilterAutoGroupsByTokenPolicy("vip", "gpt-default", baseGroups, policy))
	assert.Equal(t, []string{"preferred", "premium"}, FilterAutoGroupsByTokenPolicy("vip", "gpt-special", baseGroups, policy))
	assert.Equal(t, baseGroups, FilterAutoGroupsByTokenPolicy("vip", "gpt-unconfigured", baseGroups, &model.TokenAutoGroupPolicy{
		ModelRules: map[string]model.TokenAutoGroupRule{
			"other": {Mode: model.TokenAutoGroupModeAllowlist, Groups: []string{"premium"}},
		},
	}))

	assert.Equal(t, []string{"cheap"}, FilterAutoGroupsByTokenPolicy("vip", "gpt-special", []string{"cheap"}, &model.TokenAutoGroupPolicy{
		DefaultRule: &model.TokenAutoGroupRule{
			Mode:   model.TokenAutoGroupModeAllowlist,
			Groups: []string{"cheap", "premium"},
		},
	}))
}
