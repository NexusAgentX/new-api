package types

const RequestDegradationReasonNonReplayableReasoning = "non_replayable_reasoning"

type RequestDegradation struct {
	Applied               bool   `json:"applied"`
	Reason                string `json:"reason"`
	DroppedReasoningItems int    `json:"dropped_reasoning_items"`
}
