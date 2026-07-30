package common

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestChannelTestFirstResponseTimeoutRemainsStickyAcrossAttempts(t *testing.T) {
	info := &RelayInfo{IsChannelTest: true}

	firstContext, monitored := info.BeginFirstResponseAttempt(context.Background(), 1)
	require.True(t, monitored)
	select {
	case <-firstContext.Done():
	case <-time.After(2 * time.Second):
		require.FailNow(t, "first response attempt did not time out")
	}

	_, monitored = info.BeginFirstResponseAttempt(context.Background(), 30)
	require.True(t, monitored)
	info.MarkFirstResponseReceived()

	result := info.EndFirstResponseAttempt()
	require.True(t, result.Monitored)
	require.True(t, result.TimedOut)
	require.Equal(t, 1, result.TimeoutSeconds)
}

func TestChannelTestProtocolFailureRemainsStickyAfterValidEvent(t *testing.T) {
	info := &RelayInfo{RequireValidFirstResponseEvent: true}
	invalidEvents := [][]byte{
		[]byte("not-json"),
		[]byte(`{}`),
		[]byte(`{"error":null}`),
		[]byte(`{"type":"response.failed"}`),
		[]byte(`{"status":503}`),
	}
	for _, event := range invalidEvents {
		require.False(t, info.SetFirstResponseTimeFromJSON(event))
	}

	require.True(t, info.SetFirstResponseTimeFromJSON([]byte(`{"type":"response.created"}`)))
	require.ErrorContains(t, info.FirstResponseProtocolError(), "invalid or error upstream event")
}

func TestChannelTestProtocolAllowsControlEvents(t *testing.T) {
	info := &RelayInfo{RequireValidFirstResponseEvent: true}
	for _, event := range [][]byte{
		nil,
		[]byte("  \n"),
		[]byte("[DONE]"),
		[]byte("PING"),
	} {
		require.False(t, info.SetFirstResponseTimeFromJSON(event))
	}

	require.NoError(t, info.FirstResponseProtocolError())
	require.True(t, info.SetFirstResponseTimeFromJSON([]byte(`{"type":"ping"}`)))
}
