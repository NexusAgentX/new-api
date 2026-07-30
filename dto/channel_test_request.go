package dto

import (
	"fmt"
	"math"
	"strings"
)

const (
	MaxChannelTestModelLength         = 512
	MaxChannelTestPromptBytes         = 32 * 1024
	MaxChannelTestOutputTokens        = 8192
	MaxChannelTestReasoningEffortSize = 64
)

// ChannelTestRequest is the bounded, ad-hoc request accepted by the channel
// test endpoint. It intentionally exposes only text-generation inputs that
// are safe to use during an administrator-triggered diagnostic request.
type ChannelTestRequest struct {
	Model        string                 `json:"model,omitempty"`
	EndpointType string                 `json:"endpoint_type,omitempty"`
	Stream       *bool                  `json:"stream,omitempty"`
	SystemPrompt string                 `json:"system_prompt,omitempty"`
	Message      string                 `json:"message,omitempty"`
	Parameters   *ChannelTestParameters `json:"parameters,omitempty"`
}

// ChannelTestParameters contains common text-generation parameters. Pointer
// fields preserve an explicitly configured zero value in the diagnostic request.
type ChannelTestParameters struct {
	MaxTokens           *uint    `json:"max_tokens,omitempty"`
	MaxCompletionTokens *uint    `json:"max_completion_tokens,omitempty"`
	MaxOutputTokens     *uint    `json:"max_output_tokens,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
	TopP                *float64 `json:"top_p,omitempty"`
	TopK                *int     `json:"top_k,omitempty"`
	FrequencyPenalty    *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64 `json:"presence_penalty,omitempty"`
	ReasoningEffort     string   `json:"reasoning_effort,omitempty"`
}

func (request *ChannelTestRequest) Validate() error {
	request.Model = strings.TrimSpace(request.Model)
	request.EndpointType = strings.TrimSpace(request.EndpointType)
	if len(request.Model) > MaxChannelTestModelLength {
		return fmt.Errorf("channel test model must be at most %d bytes", MaxChannelTestModelLength)
	}
	if len(request.SystemPrompt) > MaxChannelTestPromptBytes {
		return fmt.Errorf("channel test system prompt must be at most %d bytes", MaxChannelTestPromptBytes)
	}
	if len(request.Message) > MaxChannelTestPromptBytes {
		return fmt.Errorf("channel test message must be at most %d bytes", MaxChannelTestPromptBytes)
	}
	if request.EndpointType != "" && !IsSupportedChannelTestEndpointType(request.EndpointType) {
		return fmt.Errorf("invalid channel test endpoint type: %s", request.EndpointType)
	}
	if request.Stream != nil && *request.Stream && IsChannelTestEndpointStreamIncompatible(request.EndpointType) {
		return fmt.Errorf("channel test endpoint type does not support streaming: %s", request.EndpointType)
	}
	if request.Parameters != nil {
		return request.Parameters.Validate()
	}
	return nil
}

func (parameters *ChannelTestParameters) Validate() error {
	if parameters.MaxTokens != nil && *parameters.MaxTokens > MaxChannelTestOutputTokens {
		return fmt.Errorf("max_tokens must be between 0 and %d", MaxChannelTestOutputTokens)
	}
	if parameters.MaxCompletionTokens != nil && *parameters.MaxCompletionTokens > MaxChannelTestOutputTokens {
		return fmt.Errorf("max_completion_tokens must be between 0 and %d", MaxChannelTestOutputTokens)
	}
	if parameters.MaxOutputTokens != nil && *parameters.MaxOutputTokens > MaxChannelTestOutputTokens {
		return fmt.Errorf("max_output_tokens must be between 0 and %d", MaxChannelTestOutputTokens)
	}
	if err := validateChannelTestFloat("temperature", parameters.Temperature, 0, 2); err != nil {
		return err
	}
	if err := validateChannelTestFloat("top_p", parameters.TopP, 0, 1); err != nil {
		return err
	}
	if err := validateChannelTestFloat("frequency_penalty", parameters.FrequencyPenalty, -2, 2); err != nil {
		return err
	}
	if err := validateChannelTestFloat("presence_penalty", parameters.PresencePenalty, -2, 2); err != nil {
		return err
	}
	if parameters.TopK != nil && (*parameters.TopK < 0 || *parameters.TopK > 1000) {
		return fmt.Errorf("top_k must be between 0 and 1000")
	}
	parameters.ReasoningEffort = strings.TrimSpace(parameters.ReasoningEffort)
	if len(parameters.ReasoningEffort) > MaxChannelTestReasoningEffortSize {
		return fmt.Errorf("reasoning_effort must be at most %d bytes", MaxChannelTestReasoningEffortSize)
	}
	return nil
}

func validateChannelTestFloat(name string, value *float64, min, max float64) error {
	if value == nil {
		return nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < min || *value > max {
		return fmt.Errorf("%s must be a finite number between %g and %g", name, min, max)
	}
	return nil
}
