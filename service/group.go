package service

import (
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func GetUserUsableGroups(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if strings.HasPrefix(specialGroup, "-:") {
					// 移除分组
					groupToRemove := strings.TrimPrefix(specialGroup, "-:")
					delete(groupsCopy, groupToRemove)
				} else if strings.HasPrefix(specialGroup, "+:") {
					// 添加分组
					groupToAdd := strings.TrimPrefix(specialGroup, "+:")
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
		// 如果userGroup不在UserUsableGroups中，返回UserUsableGroups + userGroup
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "用户分组"
		}
	}
	return groupsCopy
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string) []string {
	groups := GetUserUsableGroups(userGroup)
	autoGroups := make([]string, 0)
	for _, group := range setting.GetAutoGroups() {
		if _, ok := groups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
}

// GetGroupsEnabledModels 按 groups 顺序获取各分组启用的模型并去重
func GetGroupsEnabledModels(groups []string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, modelName := range model.GetGroupEnabledModels(group) {
			if _, ok := seen[modelName]; !ok {
				seen[modelName] = struct{}{}
				models = append(models, modelName)
			}
		}
	}
	return models
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}

// GetUserAutoGroupsForModel returns the ordered auto-group candidates after a
// token policy is applied. The caller supplies the effective routed model.
func GetUserAutoGroupsForModel(userGroup, modelName string, policy *model.TokenAutoGroupPolicy) []string {
	groups := GetUserAutoGroup(userGroup)
	if policy == nil {
		return groups
	}
	rule, ok := policy.RuleForModel(modelName)
	if !ok {
		return groups
	}

	allowedGroups := make(map[string]struct{}, len(rule.Groups))
	for _, group := range rule.Groups {
		allowedGroups[group] = struct{}{}
	}
	filtered := make([]string, 0, len(groups))
	for _, group := range groups {
		if rule.Mode == model.TokenAutoGroupModeAllowlist {
			if _, ok := allowedGroups[group]; !ok {
				continue
			}
		} else if rule.Mode == model.TokenAutoGroupModeDenylist {
			if _, ok := allowedGroups[group]; ok {
				continue
			}
		}

		ratio := GetUserGroupRatio(userGroup, group)
		if rule.MinRatio != nil && ratio < *rule.MinRatio {
			continue
		}
		if rule.MaxRatio != nil && ratio > *rule.MaxRatio {
			continue
		}
		filtered = append(filtered, group)
	}
	return filtered
}

func FilterUserModelsByAutoGroupPolicy(userGroup string, modelNames []string, policy *model.TokenAutoGroupPolicy) []string {
	if policy == nil || len(modelNames) == 0 {
		return modelNames
	}

	candidateGroups := make(map[string]struct{})
	for _, modelName := range modelNames {
		for _, group := range GetUserAutoGroupsForModel(userGroup, modelName, policy) {
			candidateGroups[group] = struct{}{}
		}
	}
	modelsByGroup := make(map[string]map[string]struct{}, len(candidateGroups))
	for group := range candidateGroups {
		availableModels := make(map[string]struct{})
		for _, modelName := range model.GetGroupEnabledModels(group) {
			availableModels[modelName] = struct{}{}
		}
		modelsByGroup[group] = availableModels
	}

	filtered := make([]string, 0, len(modelNames))
	for _, modelName := range modelNames {
		for _, group := range GetUserAutoGroupsForModel(userGroup, modelName, policy) {
			if _, ok := modelsByGroup[group][modelName]; ok {
				filtered = append(filtered, modelName)
				break
			}
		}
	}
	return filtered
}
