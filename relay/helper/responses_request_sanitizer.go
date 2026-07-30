package helper

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

// SanitizeResponsesRequest removes reasoning history that cannot be replayed
// in an explicitly stateless Responses request. It updates both the parsed
// request and the reusable raw body so converted and passthrough attempts use
// the same sanitized input.
func SanitizeResponsesRequest(c *gin.Context, request *dto.OpenAIResponsesRequest) (*types.RequestDegradation, error) {
	if c == nil || request == nil {
		return nil, nil
	}
	if common.GetJsonType(request.Store) != "boolean" {
		return nil, nil
	}
	var store bool
	if err := common.Unmarshal(request.Store, &store); err != nil {
		return nil, fmt.Errorf("decode responses store field: %w", err)
	}
	if store || common.GetJsonType(request.Input) != "array" {
		return nil, nil
	}

	var inputItems []json.RawMessage
	if err := common.Unmarshal(request.Input, &inputItems); err != nil {
		return nil, fmt.Errorf("decode responses input array: %w", err)
	}

	type inputItem struct {
		Type             string          `json:"type"`
		EncryptedContent json.RawMessage `json:"encrypted_content"`
	}

	filteredItems := make([]json.RawMessage, 0, len(inputItems))
	droppedItems := 0
	for _, rawItem := range inputItems {
		if common.GetJsonType(rawItem) != "object" {
			filteredItems = append(filteredItems, rawItem)
			continue
		}

		var item inputItem
		if err := common.Unmarshal(rawItem, &item); err != nil || item.Type != "reasoning" {
			filteredItems = append(filteredItems, rawItem)
			continue
		}

		dropItem := len(item.EncryptedContent) == 0 || common.GetJsonType(item.EncryptedContent) == "null"
		if common.GetJsonType(item.EncryptedContent) == "string" {
			var encryptedContent string
			if err := common.Unmarshal(item.EncryptedContent, &encryptedContent); err != nil {
				return nil, fmt.Errorf("decode reasoning encrypted_content: %w", err)
			}
			dropItem = encryptedContent == ""
		}
		if dropItem {
			droppedItems++
			continue
		}
		filteredItems = append(filteredItems, rawItem)
	}

	if droppedItems == 0 {
		return nil, nil
	}

	filteredInput, err := common.Marshal(filteredItems)
	if err != nil {
		return nil, fmt.Errorf("encode sanitized responses input: %w", err)
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, fmt.Errorf("read reusable responses body: %w", err)
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, fmt.Errorf("read responses body bytes: %w", err)
	}
	filteredBody, err := sjson.SetRawBytes(body, "input", filteredInput)
	if err != nil {
		return nil, fmt.Errorf("replace responses input in request body: %w", err)
	}
	if err := common.ReplaceBodyStorage(c, filteredBody); err != nil {
		return nil, fmt.Errorf("store sanitized responses body: %w", err)
	}

	request.Input = filteredInput
	return &types.RequestDegradation{
		Applied:               true,
		Reason:                types.RequestDegradationReasonNonReplayableReasoning,
		DroppedReasoningItems: droppedItems,
	}, nil
}
