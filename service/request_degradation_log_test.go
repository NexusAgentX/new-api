package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoRecordsRequestDegradationAtTopLevel(t *testing.T) {
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		ChannelMeta:       &relaycommon.ChannelMeta{},
		RequestDegradation: &types.RequestDegradation{
			Applied:               true,
			Reason:                types.RequestDegradationReasonNonReplayableReasoning,
			DroppedReasoningItems: 4,
		},
		ResponsesCompatibility: &types.ResponsesCompatibility{
			DroppedNonReplayableReasoningItems: 4,
			NormalizedRequestItemIDs:           2,
			NormalizedResponseItemIDs:          3,
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 1, 0, 1)
	encoded, err := common.Marshal(other)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(encoded, &decoded))

	degradation, ok := decoded["request_degradation"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, degradation["applied"])
	assert.Equal(t, types.RequestDegradationReasonNonReplayableReasoning, degradation["reason"])
	assert.Equal(t, float64(4), degradation["dropped_reasoning_items"])
	compatibility, ok := decoded["responses_compatibility"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(4), compatibility["dropped_non_replayable_reasoning_items"])
	assert.Equal(t, float64(2), compatibility["normalized_request_item_ids"])
	assert.Equal(t, float64(3), compatibility["normalized_response_item_ids"])
	assert.Len(t, compatibility, 3)

	adminInfo, ok := decoded["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, adminInfo, "request_degradation")
	assert.NotContains(t, adminInfo, "responses_compatibility")
}

func TestAppendResponsesCompatibilityInfoOmitsEmptyMetadata(t *testing.T) {
	for _, compatibility := range []*types.ResponsesCompatibility{nil, {}} {
		other := map[string]any{}
		AppendResponsesCompatibilityInfo(&relaycommon.RelayInfo{ResponsesCompatibility: compatibility}, other)
		assert.NotContains(t, other, "responses_compatibility")
	}
}

func TestAppendRequestDegradationInfoOmitsUnappliedMetadata(t *testing.T) {
	testCases := []struct {
		name        string
		degradation *types.RequestDegradation
	}{
		{name: "missing"},
		{name: "not applied", degradation: &types.RequestDegradation{Reason: types.RequestDegradationReasonNonReplayableReasoning, DroppedReasoningItems: 1}},
		{name: "zero dropped", degradation: &types.RequestDegradation{Applied: true, Reason: types.RequestDegradationReasonNonReplayableReasoning}},
		{name: "missing reason", degradation: &types.RequestDegradation{Applied: true, DroppedReasoningItems: 1}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			other := map[string]any{}
			AppendRequestDegradationInfo(&relaycommon.RelayInfo{RequestDegradation: testCase.degradation}, other)
			assert.NotContains(t, other, "request_degradation")
		})
	}
}
