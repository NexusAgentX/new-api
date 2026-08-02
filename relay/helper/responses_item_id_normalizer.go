package helper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type ResponsesItemIDNormalizer struct {
	mappings   map[string]string
	used       map[string]struct{}
	normalized map[string]struct{}
}

func NewResponsesItemIDNormalizer() *ResponsesItemIDNormalizer {
	return &ResponsesItemIDNormalizer{
		mappings:   make(map[string]string),
		used:       make(map[string]struct{}),
		normalized: make(map[string]struct{}),
	}
}

func (n *ResponsesItemIDNormalizer) NormalizedCount() int {
	if n == nil {
		return 0
	}
	return len(n.normalized)
}

func responsesItemIDPrefix(itemType string) string {
	switch itemType {
	case "function_call":
		return "fc_"
	case "message":
		return "msg_"
	case "reasoning":
		return "rs_"
	default:
		return ""
	}
}

func responsesItemIDSuffix(id string) string {
	for _, prefix := range []string{"item_", "fc_", "msg_", "rs_"} {
		if strings.HasPrefix(id, prefix) && len(id) > len(prefix) {
			return strings.TrimPrefix(id, prefix)
		}
	}
	return id
}

func (n *ResponsesItemIDNormalizer) normalizeID(itemType string, id string) string {
	if n == nil || id == "" {
		return id
	}
	if mapped, ok := n.mappings[id]; ok {
		return mapped
	}

	prefix := responsesItemIDPrefix(itemType)
	if prefix == "" {
		return id
	}
	if strings.HasPrefix(id, prefix) {
		n.mappings[id] = id
		n.used[id] = struct{}{}
		return id
	}

	candidate := prefix + responsesItemIDSuffix(id)
	if _, exists := n.used[candidate]; exists {
		hash := sha256.Sum256([]byte(itemType + "\x00" + id))
		candidate = prefix + hex.EncodeToString(hash[:12])
		for suffix := 2; ; suffix++ {
			if _, exists = n.used[candidate]; !exists {
				break
			}
			candidate = prefix + hex.EncodeToString(hash[:12]) + "_" + strconv.Itoa(suffix)
		}
	}

	n.mappings[id] = candidate
	n.used[candidate] = struct{}{}
	n.normalized[id] = struct{}{}
	return candidate
}

func decodeJSONString(raw json.RawMessage) (string, bool) {
	if common.GetJsonType(raw) != "string" {
		return "", false
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func reserveResponsesItemID(raw json.RawMessage, normalizer *ResponsesItemIDNormalizer) {
	if normalizer == nil || common.GetJsonType(raw) != "object" {
		return
	}
	var item map[string]json.RawMessage
	if err := common.Unmarshal(raw, &item); err != nil {
		return
	}
	if id, ok := decodeJSONString(item["id"]); ok && id != "" {
		normalizer.used[id] = struct{}{}
	}
}

func reserveResponsesOutputIDs(raw json.RawMessage, normalizer *ResponsesItemIDNormalizer) {
	if normalizer == nil || common.GetJsonType(raw) != "array" {
		return
	}
	var items []json.RawMessage
	if err := common.Unmarshal(raw, &items); err != nil {
		return
	}
	for _, item := range items {
		reserveResponsesItemID(item, normalizer)
	}
}

func normalizeResponsesItem(raw json.RawMessage, normalizer *ResponsesItemIDNormalizer) (json.RawMessage, bool, error) {
	if normalizer == nil || common.GetJsonType(raw) != "object" {
		return raw, false, nil
	}

	var item map[string]json.RawMessage
	if err := common.Unmarshal(raw, &item); err != nil {
		return nil, false, fmt.Errorf("decode responses item: %w", err)
	}
	itemType, typeOK := decodeJSONString(item["type"])
	id, idOK := decodeJSONString(item["id"])
	if !typeOK || !idOK || id == "" || responsesItemIDPrefix(itemType) == "" {
		return raw, false, nil
	}

	normalizedID := normalizer.normalizeID(itemType, id)
	if normalizedID == id {
		return raw, false, nil
	}
	encodedID, err := common.Marshal(normalizedID)
	if err != nil {
		return nil, false, fmt.Errorf("encode normalized responses item id: %w", err)
	}
	item["id"] = encodedID
	normalizedItem, err := common.Marshal(item)
	if err != nil {
		return nil, false, fmt.Errorf("encode normalized responses item: %w", err)
	}
	return normalizedItem, true, nil
}

func normalizeResponsesOutput(raw json.RawMessage, normalizer *ResponsesItemIDNormalizer) (json.RawMessage, bool, error) {
	if common.GetJsonType(raw) != "array" {
		return raw, false, nil
	}
	var items []json.RawMessage
	if err := common.Unmarshal(raw, &items); err != nil {
		return nil, false, fmt.Errorf("decode responses output: %w", err)
	}

	reserveResponsesOutputIDs(raw, normalizer)
	changed := false
	for index, item := range items {
		normalizedItem, itemChanged, err := normalizeResponsesItem(item, normalizer)
		if err != nil {
			return nil, false, err
		}
		if itemChanged {
			items[index] = normalizedItem
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	normalizedOutput, err := common.Marshal(items)
	if err != nil {
		return nil, false, fmt.Errorf("encode normalized responses output: %w", err)
	}
	return normalizedOutput, true, nil
}

func normalizeResponsesObject(raw json.RawMessage, normalizer *ResponsesItemIDNormalizer) (json.RawMessage, bool, error) {
	if common.GetJsonType(raw) != "object" {
		return raw, false, nil
	}
	var response map[string]json.RawMessage
	if err := common.Unmarshal(raw, &response); err != nil {
		return nil, false, fmt.Errorf("decode responses object: %w", err)
	}
	normalizedOutput, changed, err := normalizeResponsesOutput(response["output"], normalizer)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return raw, false, nil
	}
	response["output"] = normalizedOutput
	normalizedResponse, err := common.Marshal(response)
	if err != nil {
		return nil, false, fmt.Errorf("encode normalized responses object: %w", err)
	}
	return normalizedResponse, true, nil
}

func NormalizeResponsesResponseJSON(data []byte, normalizer *ResponsesItemIDNormalizer) ([]byte, int, error) {
	if normalizer == nil {
		return data, 0, nil
	}
	before := normalizer.NormalizedCount()
	normalized, changed, err := normalizeResponsesObject(data, normalizer)
	if err != nil {
		return nil, 0, err
	}
	if !changed {
		return data, normalizer.NormalizedCount() - before, nil
	}
	return normalized, normalizer.NormalizedCount() - before, nil
}

func responsesStreamEventItemType(eventType string) string {
	switch {
	case strings.Contains(eventType, "function_call_arguments"):
		return "function_call"
	case strings.Contains(eventType, "reasoning"):
		return "reasoning"
	case strings.Contains(eventType, "output_text"):
		return "message"
	default:
		return ""
	}
}

func NormalizeResponsesStreamEventJSON(data []byte, normalizer *ResponsesItemIDNormalizer) ([]byte, int, error) {
	if normalizer == nil {
		return data, 0, nil
	}
	before := normalizer.NormalizedCount()
	var event map[string]json.RawMessage
	if err := common.Unmarshal(data, &event); err != nil {
		return nil, 0, fmt.Errorf("decode responses stream event: %w", err)
	}

	reserveResponsesItemID(event["item"], normalizer)
	if responseRaw := event["response"]; common.GetJsonType(responseRaw) == "object" {
		var response map[string]json.RawMessage
		if err := common.Unmarshal(responseRaw, &response); err == nil {
			reserveResponsesOutputIDs(response["output"], normalizer)
		}
	}

	changed := false
	if itemRaw, exists := event["item"]; exists {
		normalizedItem, itemChanged, err := normalizeResponsesItem(itemRaw, normalizer)
		if err != nil {
			return nil, 0, err
		}
		if itemChanged {
			event["item"] = normalizedItem
			changed = true
		}
	}
	if responseRaw, exists := event["response"]; exists {
		normalizedResponse, responseChanged, err := normalizeResponsesObject(responseRaw, normalizer)
		if err != nil {
			return nil, 0, err
		}
		if responseChanged {
			event["response"] = normalizedResponse
			changed = true
		}
	}

	if itemID, ok := decodeJSONString(event["item_id"]); ok && itemID != "" {
		normalizedID := itemID
		if mapped, exists := normalizer.mappings[itemID]; exists {
			normalizedID = mapped
		} else if eventType, typeOK := decodeJSONString(event["type"]); typeOK {
			normalizedID = normalizer.normalizeID(responsesStreamEventItemType(eventType), itemID)
		}
		if normalizedID != itemID {
			encodedID, err := common.Marshal(normalizedID)
			if err != nil {
				return nil, 0, fmt.Errorf("encode normalized responses stream item id: %w", err)
			}
			event["item_id"] = encodedID
			changed = true
		}
	}

	if !changed {
		return data, normalizer.NormalizedCount() - before, nil
	}
	normalizedEvent, err := common.Marshal(event)
	if err != nil {
		return nil, 0, fmt.Errorf("encode normalized responses stream event: %w", err)
	}
	return normalizedEvent, normalizer.NormalizedCount() - before, nil
}
