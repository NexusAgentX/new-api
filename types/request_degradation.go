package types

const RequestDegradationReasonNonReplayableReasoning = "non_replayable_reasoning"

type RequestDegradation struct {
	Applied               bool   `json:"applied"`
	Reason                string `json:"reason"`
	DroppedReasoningItems int    `json:"dropped_reasoning_items"`
}

type ResponsesCompatibility struct {
	DroppedNonReplayableReasoningItems int `json:"dropped_non_replayable_reasoning_items"`
	NormalizedRequestItemIDs           int `json:"normalized_request_item_ids"`
	NormalizedResponseItemIDs          int `json:"normalized_response_item_ids"`
}

func (c *ResponsesCompatibility) Applied() bool {
	return c != nil && (c.DroppedNonReplayableReasoningItems > 0 ||
		c.NormalizedRequestItemIDs > 0 || c.NormalizedResponseItemIDs > 0)
}
