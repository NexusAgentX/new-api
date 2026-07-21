package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/tidwall/gjson"
)

const (
	tokenRequestCustomizationVersion  = 1
	maxTokenRequestCustomizationBytes = 16 * 1024
	maxTokenModelMappings             = 100
	maxTokenModelNameBytes            = 256
)

type TokenRequestCustomization struct {
	Version      int               `json:"version"`
	ModelMapping map[string]string `json:"model_mapping,omitempty"`
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
		if field != "version" && field != "model_mapping" {
			return customization, fmt.Errorf("unsupported request customization field %q", field)
		}
	}
	if _, exists := fields["version"]; !exists {
		return customization, errors.New("request customization version is required")
	}
	if modelMapping, exists := fields["model_mapping"]; exists && common.GetJsonType(modelMapping) != "object" {
		return customization, errors.New("model mapping must be a JSON object")
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
		return customization, fmt.Errorf("duplicate model mapping source %q", duplicateSource)
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

	for source := range customization.ModelMapping {
		if _, _, err := ResolveTokenModelMapping(customization.ModelMapping, source); err != nil {
			return customization, err
		}
	}
	return customization, nil
}

func NormalizeTokenRequestCustomization(raw string) (string, error) {
	customization, err := ParseTokenRequestCustomization(raw)
	if err != nil {
		return "", err
	}
	if len(customization.ModelMapping) == 0 {
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
