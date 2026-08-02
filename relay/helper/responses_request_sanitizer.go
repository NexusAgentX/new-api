package helper

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
)

type ResponsesRequestAttempt struct {
	Request       *dto.OpenAIResponsesRequest
	Compatibility *hosttypes.ResponsesCompatibility
	Degradation   *hosttypes.RequestDegradation
}

func PrepareResponsesRequestAttempt(
	original *dto.OpenAIResponsesRequest,
	settings dto.ChannelSettings,
	passThrough bool,
) (*ResponsesRequestAttempt, error) {
	if original == nil {
		return nil, fmt.Errorf("responses request is nil")
	}
	request, err := common.DeepCopy(original)
	if err != nil {
		return nil, fmt.Errorf("copy responses request: %w", err)
	}
	attempt := &ResponsesRequestAttempt{Request: request}
	if passThrough || !settings.ResponsesCompatibilityFixEnabled() {
		return attempt, nil
	}
	if common.GetJsonType(request.Store) != "boolean" {
		return attempt, nil
	}
	var store bool
	if err := common.Unmarshal(request.Store, &store); err != nil {
		return nil, fmt.Errorf("decode responses store field: %w", err)
	}
	if store || common.GetJsonType(request.Input) != "array" {
		return attempt, nil
	}

	var inputItems []json.RawMessage
	if err := common.Unmarshal(request.Input, &inputItems); err != nil {
		return nil, fmt.Errorf("decode responses input array: %w", err)
	}

	filteredItems := make([]json.RawMessage, 0, len(inputItems))
	droppedItems := 0
	for _, rawItem := range inputItems {
		drop, err := shouldDropResponsesReasoningItem(
			rawItem,
			settings.AllowsReasoningWithoutEncryptedContent(),
		)
		if err != nil {
			return nil, err
		}
		if drop {
			droppedItems++
			continue
		}
		filteredItems = append(filteredItems, rawItem)
	}

	normalizer := NewResponsesItemIDNormalizer()
	for _, rawItem := range filteredItems {
		reserveResponsesItemID(rawItem, normalizer)
	}
	for index, rawItem := range filteredItems {
		normalizedItem, changed, err := normalizeResponsesItem(rawItem, normalizer)
		if err != nil {
			return nil, err
		}
		if changed {
			filteredItems[index] = normalizedItem
		}
	}
	normalizedIDs := normalizer.NormalizedCount()
	if droppedItems == 0 && normalizedIDs == 0 {
		return attempt, nil
	}

	filteredInput, err := common.Marshal(filteredItems)
	if err != nil {
		return nil, fmt.Errorf("encode compatible responses input: %w", err)
	}
	request.Input = filteredInput
	attempt.Compatibility = &hosttypes.ResponsesCompatibility{
		DroppedNonReplayableReasoningItems: droppedItems,
		NormalizedRequestItemIDs:           normalizedIDs,
	}
	if droppedItems > 0 {
		attempt.Degradation = &hosttypes.RequestDegradation{
			Applied:               true,
			Reason:                hosttypes.RequestDegradationReasonNonReplayableReasoning,
			DroppedReasoningItems: droppedItems,
		}
	}
	return attempt, nil
}

func shouldDropResponsesReasoningItem(rawItem json.RawMessage, allowWithoutEncryptedContent bool) (bool, error) {
	if allowWithoutEncryptedContent || common.GetJsonType(rawItem) != "object" {
		return false, nil
	}
	var item map[string]json.RawMessage
	if err := common.Unmarshal(rawItem, &item); err != nil {
		return false, nil
	}
	itemType, ok := decodeJSONString(item["type"])
	if !ok || itemType != "reasoning" {
		return false, nil
	}

	encryptedContent, exists := item["encrypted_content"]
	if !exists || len(encryptedContent) == 0 || common.GetJsonType(encryptedContent) == "null" {
		return true, nil
	}
	if common.GetJsonType(encryptedContent) != "string" {
		return false, nil
	}
	var value string
	if err := common.Unmarshal(encryptedContent, &value); err != nil {
		return false, fmt.Errorf("decode reasoning encrypted_content: %w", err)
	}
	return value == "", nil
}
