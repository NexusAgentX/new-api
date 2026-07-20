package ratio_setting

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
)

var defaultGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}

var groupRatioMap = types.NewRWMap[string, float64]()
var groupAllowZeroQuotaMap = types.NewRWMap[string, bool]()
var groupCreditQuotaMap = types.NewRWMap[string, int]()

var defaultGroupGroupRatio = map[string]map[string]float64{
	"vip": {
		"edit_this": 0.9,
	},
}

var groupGroupRatioMap = types.NewRWMap[string, map[string]float64]()

var defaultGroupSpecialUsableGroup = map[string]map[string]string{}

type GroupRatioSetting struct {
	GroupRatio              *types.RWMap[string, float64]            `json:"group_ratio"`
	GroupAllowZeroQuota     *types.RWMap[string, bool]               `json:"group_allow_zero_quota"`
	GroupCreditQuota        *types.RWMap[string, int]                `json:"group_credit_quota"`
	GroupGroupRatio         *types.RWMap[string, map[string]float64] `json:"group_group_ratio"`
	GroupSpecialUsableGroup *types.RWMap[string, map[string]string]  `json:"group_special_usable_group"`
}

var groupRatioSetting GroupRatioSetting

func init() {
	groupSpecialUsableGroup := types.NewRWMap[string, map[string]string]()
	groupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)

	groupRatioMap.AddAll(defaultGroupRatio)
	groupGroupRatioMap.AddAll(defaultGroupGroupRatio)

	groupRatioSetting = GroupRatioSetting{
		GroupSpecialUsableGroup: groupSpecialUsableGroup,
		GroupRatio:              groupRatioMap,
		GroupAllowZeroQuota:     groupAllowZeroQuotaMap,
		GroupCreditQuota:        groupCreditQuotaMap,
		GroupGroupRatio:         groupGroupRatioMap,
	}

	config.GlobalConfig.Register("group_ratio_setting", &groupRatioSetting)
}

func GetGroupRatioSetting() *GroupRatioSetting {
	if groupRatioSetting.GroupSpecialUsableGroup == nil {
		groupRatioSetting.GroupSpecialUsableGroup = types.NewRWMap[string, map[string]string]()
		groupRatioSetting.GroupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)
	}
	if groupRatioSetting.GroupAllowZeroQuota == nil {
		groupRatioSetting.GroupAllowZeroQuota = groupAllowZeroQuotaMap
	}
	if groupRatioSetting.GroupCreditQuota == nil {
		groupRatioSetting.GroupCreditQuota = groupCreditQuotaMap
	}
	return &groupRatioSetting
}

func GetGroupRatioCopy() map[string]float64 {
	return groupRatioMap.ReadAll()
}

func ContainsGroupRatio(name string) bool {
	_, ok := groupRatioMap.Get(name)
	return ok
}

func GroupRatio2JSONString() string {
	return groupRatioMap.MarshalJSONString()
}

func UpdateGroupRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(groupRatioMap, jsonStr)
}

func GetGroupRatio(name string) float64 {
	ratio, ok := groupRatioMap.Get(name)
	if !ok {
		common.SysLog("group ratio not found: " + name)
		return 1
	}
	return ratio
}

func IsZeroQuotaAllowed(name string) bool {
	allowed, ok := groupAllowZeroQuotaMap.Get(name)
	return ok && allowed
}

func GetGroupCreditQuota(name string) int {
	quota, ok := groupCreditQuotaMap.Get(name)
	if !ok || quota < 0 || quota > common.MaxQuota {
		return 0
	}
	return quota
}

func GetGroupAvailableQuota(name string, balance int) int64 {
	return int64(balance) + int64(GetGroupCreditQuota(name))
}

func GetGroupGroupRatio(userGroup, usingGroup string) (float64, bool) {
	gp, ok := groupGroupRatioMap.Get(userGroup)
	if !ok {
		return -1, false
	}
	ratio, ok := gp[usingGroup]
	if !ok {
		return -1, false
	}
	return ratio, true
}

func GroupGroupRatio2JSONString() string {
	return groupGroupRatioMap.MarshalJSONString()
}

func UpdateGroupGroupRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(groupGroupRatioMap, jsonStr)
}

func CheckGroupRatio(jsonStr string) error {
	checkGroupRatio := make(map[string]float64)
	if err := common.Unmarshal([]byte(jsonStr), &checkGroupRatio); err != nil {
		return err
	}
	if checkGroupRatio == nil {
		return errors.New("group ratio must be a JSON object")
	}
	for name, ratio := range checkGroupRatio {
		if ratio < 0 {
			return errors.New("group ratio must be not less than 0: " + name)
		}
	}
	return nil
}

func CheckGroupAllowZeroQuota(jsonStr string) error {
	settings := make(map[string]bool)
	if err := common.Unmarshal([]byte(jsonStr), &settings); err != nil {
		return err
	}
	if settings == nil {
		return errors.New("group zero quota setting must be a JSON object")
	}
	return nil
}

func CheckGroupCreditQuota(jsonStr string) error {
	settings := make(map[string]int)
	if err := common.Unmarshal([]byte(jsonStr), &settings); err != nil {
		return err
	}
	if settings == nil {
		return errors.New("group credit quota setting must be a JSON object")
	}
	for name, quota := range settings {
		if quota < 0 {
			return errors.New("group credit quota must be not less than 0: " + name)
		}
		if quota > common.MaxQuota {
			return errors.New("group credit quota exceeds maximum quota: " + name)
		}
	}
	return nil
}
