package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/tidwall/gjson"
)

const (
	tokenRequestCustomizationVersion  = 1
	maxTokenRequestCustomizationBytes = 16 * 1024
	maxTokenModelMappings             = 100
	maxTokenModelNameBytes            = 256
	maxTokenAutoGroupModelRules       = 100
	maxTokenAutoGroupGroups           = 100
	maxTokenAutoGroupNameBytes        = 64
)

const (
	TokenAutoGroupModeAllowlist = "allowlist"
	TokenAutoGroupModeDenylist  = "denylist"
)

type TokenAutoGroupRule struct {
	Mode     string   `json:"mode,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	MinRatio *float64 `json:"min_ratio,omitempty"`
	MaxRatio *float64 `json:"max_ratio,omitempty"`
}

type TokenAutoGroupPolicy struct {
	DefaultRule *TokenAutoGroupRule           `json:"default_rule,omitempty"`
	ModelRules  map[string]TokenAutoGroupRule `json:"model_rules,omitempty"`
}

func (p *TokenAutoGroupPolicy) RuleForModel(modelName string) (TokenAutoGroupRule, bool) {
	if p == nil {
		return TokenAutoGroupRule{}, false
	}
	if rule, ok := p.ModelRules[modelName]; ok {
		return rule, true
	}
	if p.DefaultRule != nil {
		return *p.DefaultRule, true
	}
	return TokenAutoGroupRule{}, false
}

type TokenRequestCustomization struct {
	Version         int                   `json:"version"`
	ModelMapping    map[string]string     `json:"model_mapping,omitempty"`
	AutoGroupPolicy *TokenAutoGroupPolicy `json:"auto_group_policy,omitempty"`
}

func ParseTokenRequestCustomization(raw string) (TokenRequestCustomization, error) {
	customization := TokenRequestCustomization{
		Version:      tokenRequestCustomizationVersion,
		ModelMapping: map[string]string{},
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return customization, nil
	}
	if len(raw) > maxTokenRequestCustomizationBytes {
		return customization, fmt.Errorf("request customization exceeds %d bytes", maxTokenRequestCustomizationBytes)
	}

	var fields map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(raw, &fields); err != nil {
		return customization, fmt.Errorf("request customization must be a JSON object: %w", err)
	}
	if fields == nil {
		return customization, errors.New("request customization must be a JSON object")
	}
	parsed := gjson.Parse(raw)
	seenFields := make(map[string]bool, len(fields))
	var duplicateField string
	parsed.ForEach(func(key, _ gjson.Result) bool {
		field := key.String()
		if seenFields[field] {
			duplicateField = field
			return false
		}
		seenFields[field] = true
		return true
	})
	if duplicateField != "" {
		return customization, fmt.Errorf("duplicate request customization field %q", duplicateField)
	}
	for field := range fields {
		if field != "version" && field != "model_mapping" && field != "auto_group_policy" {
			return customization, fmt.Errorf("unsupported request customization field %q", field)
		}
	}
	if _, exists := fields["version"]; !exists {
		return customization, errors.New("request customization version is required")
	}
	if modelMapping, exists := fields["model_mapping"]; exists && common.GetJsonType(modelMapping) != "object" {
		return customization, errors.New("model mapping must be a JSON object")
	}
	if policy, exists := fields["auto_group_policy"]; exists {
		if common.GetJsonType(policy) != "object" {
			return customization, errors.New("auto group policy must be a JSON object")
		}
		if err := validateTokenAutoGroupPolicyJSON(policy); err != nil {
			return customization, err
		}
	}
	if err := common.UnmarshalJsonStr(raw, &customization); err != nil {
		return customization, fmt.Errorf("invalid request customization: %w", err)
	}
	if customization.Version != tokenRequestCustomizationVersion {
		return customization, fmt.Errorf("unsupported request customization version %d", customization.Version)
	}
	if len(customization.ModelMapping) > maxTokenModelMappings {
		return customization, fmt.Errorf("model mapping exceeds %d entries", maxTokenModelMappings)
	}
	seenSources := make(map[string]bool, len(customization.ModelMapping))
	var duplicateSource string
	parsed.Get("model_mapping").ForEach(func(key, _ gjson.Result) bool {
		source := key.String()
		if seenSources[source] {
			duplicateSource = source
			return false
		}
		seenSources[source] = true
		return true
	})
	if duplicateSource != "" {
		return customization, fmt.Errorf("duplicate request customization model mapping source %q", duplicateSource)
	}

	normalizedMapping := make(map[string]string, len(customization.ModelMapping))
	for source, target := range customization.ModelMapping {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			return customization, errors.New("model mapping source and target must not be empty")
		}
		if len(source) > maxTokenModelNameBytes || len(target) > maxTokenModelNameBytes {
			return customization, fmt.Errorf("model names must not exceed %d bytes", maxTokenModelNameBytes)
		}
		if _, exists := normalizedMapping[source]; exists {
			return customization, fmt.Errorf("duplicate model mapping source %q", source)
		}
		normalizedMapping[source] = target
	}
	customization.ModelMapping = normalizedMapping

	if customization.AutoGroupPolicy != nil {
		policy, err := normalizeTokenAutoGroupPolicy(*customization.AutoGroupPolicy)
		if err != nil {
			return customization, err
		}
		customization.AutoGroupPolicy = policy
	}

	for source := range customization.ModelMapping {
		if _, _, err := ResolveTokenModelMapping(customization.ModelMapping, source); err != nil {
			return customization, err
		}
	}
	return customization, nil
}

func validateTokenAutoGroupPolicyJSON(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("auto group policy must be a JSON object")
	}
	for field, value := range fields {
		switch field {
		case "default_rule":
			if common.GetJsonType(value) != "object" {
				return errors.New("auto group policy default_rule must be an object")
			}
			if err := validateTokenAutoGroupRuleJSON(value); err != nil {
				return fmt.Errorf("auto group policy default_rule: %w", err)
			}
		case "model_rules":
			if common.GetJsonType(value) != "object" {
				return errors.New("auto group policy model_rules must be an object")
			}
			var rules map[string]json.RawMessage
			if err := common.Unmarshal(value, &rules); err != nil {
				return fmt.Errorf("auto group policy model_rules: %w", err)
			}
			if len(rules) > maxTokenAutoGroupModelRules {
				return fmt.Errorf("auto group policy exceeds %d model rules", maxTokenAutoGroupModelRules)
			}
			for modelName, rule := range rules {
				if strings.TrimSpace(modelName) == "" {
					return errors.New("auto group policy model rule name must not be empty")
				}
				if common.GetJsonType(rule) != "object" {
					return fmt.Errorf("auto group policy model rule %q must be an object", modelName)
				}
				if err := validateTokenAutoGroupRuleJSON(rule); err != nil {
					return fmt.Errorf("auto group policy model rule %q: %w", modelName, err)
				}
			}
		default:
			return fmt.Errorf("unsupported auto group policy field %q", field)
		}
	}
	return nil
}

func validateTokenAutoGroupRuleJSON(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("rule must be a JSON object")
	}
	for field := range fields {
		switch field {
		case "mode", "groups", "min_ratio", "max_ratio":
		default:
			return fmt.Errorf("unsupported rule field %q", field)
		}
	}
	return nil
}

func normalizeTokenAutoGroupPolicy(policy TokenAutoGroupPolicy) (*TokenAutoGroupPolicy, error) {
	if policy.DefaultRule == nil && len(policy.ModelRules) == 0 {
		return nil, nil
	}

	normalized := &TokenAutoGroupPolicy{}
	if policy.DefaultRule != nil {
		rule, err := normalizeTokenAutoGroupRule(*policy.DefaultRule)
		if err != nil {
			return nil, fmt.Errorf("auto group default rule: %w", err)
		}
		if tokenAutoGroupRuleHasConstraint(rule) {
			normalized.DefaultRule = &rule
		}
	}
	if len(policy.ModelRules) > maxTokenAutoGroupModelRules {
		return nil, fmt.Errorf("auto group policy exceeds %d model rules", maxTokenAutoGroupModelRules)
	}
	normalized.ModelRules = make(map[string]TokenAutoGroupRule, len(policy.ModelRules))
	seenModels := make(map[string]struct{}, len(policy.ModelRules))
	for modelName, rule := range policy.ModelRules {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			return nil, errors.New("auto group policy model rule name must not be empty")
		}
		if len(modelName) > maxTokenModelNameBytes {
			return nil, fmt.Errorf("auto group policy model name must not exceed %d bytes", maxTokenModelNameBytes)
		}
		if _, exists := seenModels[modelName]; exists {
			return nil, fmt.Errorf("duplicate auto group policy model rule %q", modelName)
		}
		seenModels[modelName] = struct{}{}
		normalizedRule, err := normalizeTokenAutoGroupRule(rule)
		if err != nil {
			return nil, fmt.Errorf("auto group policy model rule %q: %w", modelName, err)
		}
		if tokenAutoGroupRuleHasConstraint(normalizedRule) {
			normalized.ModelRules[modelName] = normalizedRule
		}
	}
	if normalized.DefaultRule == nil && len(normalized.ModelRules) == 0 {
		return nil, nil
	}
	return normalized, nil
}

func tokenAutoGroupRuleHasConstraint(rule TokenAutoGroupRule) bool {
	return rule.Mode != "" || rule.MinRatio != nil || rule.MaxRatio != nil
}

func normalizeTokenAutoGroupRule(rule TokenAutoGroupRule) (TokenAutoGroupRule, error) {
	rule.Mode = strings.TrimSpace(rule.Mode)
	if rule.Mode != "" && rule.Mode != TokenAutoGroupModeAllowlist && rule.Mode != TokenAutoGroupModeDenylist {
		return TokenAutoGroupRule{}, fmt.Errorf("mode must be %q or %q", TokenAutoGroupModeAllowlist, TokenAutoGroupModeDenylist)
	}
	if len(rule.Groups) > maxTokenAutoGroupGroups {
		return TokenAutoGroupRule{}, fmt.Errorf("groups exceed %d entries", maxTokenAutoGroupGroups)
	}
	normalizedGroups := make([]string, 0, len(rule.Groups))
	seenGroups := make(map[string]struct{}, len(rule.Groups))
	for _, group := range rule.Groups {
		group = strings.TrimSpace(group)
		if group == "" {
			return TokenAutoGroupRule{}, errors.New("group names must not be empty")
		}
		if len(group) > maxTokenAutoGroupNameBytes {
			return TokenAutoGroupRule{}, fmt.Errorf("group names must not exceed %d bytes", maxTokenAutoGroupNameBytes)
		}
		if _, exists := seenGroups[group]; exists {
			return TokenAutoGroupRule{}, fmt.Errorf("duplicate group %q", group)
		}
		seenGroups[group] = struct{}{}
		normalizedGroups = append(normalizedGroups, group)
	}
	if len(normalizedGroups) == 0 {
		rule.Mode = ""
	}
	if rule.Mode == "" && len(normalizedGroups) > 0 {
		return TokenAutoGroupRule{}, errors.New("mode is required when groups are configured")
	}
	rule.Groups = normalizedGroups
	if rule.MinRatio != nil {
		if math.IsNaN(*rule.MinRatio) || math.IsInf(*rule.MinRatio, 0) || *rule.MinRatio < 0 {
			return TokenAutoGroupRule{}, errors.New("min_ratio must be a finite non-negative number")
		}
	}
	if rule.MaxRatio != nil {
		if math.IsNaN(*rule.MaxRatio) || math.IsInf(*rule.MaxRatio, 0) || *rule.MaxRatio < 0 {
			return TokenAutoGroupRule{}, errors.New("max_ratio must be a finite non-negative number")
		}
	}
	if rule.MinRatio != nil && rule.MaxRatio != nil && *rule.MinRatio > *rule.MaxRatio {
		return TokenAutoGroupRule{}, errors.New("min_ratio must not be greater than max_ratio")
	}
	return rule, nil
}

func NormalizeTokenRequestCustomization(raw string) (string, error) {
	customization, err := ParseTokenRequestCustomization(raw)
	if err != nil {
		return "", err
	}
	if len(customization.ModelMapping) == 0 && customization.AutoGroupPolicy == nil {
		return "", nil
	}
	data, err := common.Marshal(customization)
	if err != nil {
		return "", fmt.Errorf("failed to normalize request customization: %w", err)
	}
	return string(data), nil
}

func ResolveTokenModelMapping(modelMapping map[string]string, modelName string) (string, bool, error) {
	currentModel := modelName
	visited := map[string]bool{currentModel: true}
	for {
		mappedModel, exists := modelMapping[currentModel]
		if !exists {
			return currentModel, currentModel != modelName, nil
		}
		if visited[mappedModel] {
			return "", false, fmt.Errorf("model mapping contains a cycle involving %q", mappedModel)
		}
		visited[mappedModel] = true
		currentModel = mappedModel
	}
}
