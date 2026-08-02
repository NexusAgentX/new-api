package helper

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var ErrNoSafeFailureSampleText = errors.New("failure sample has no safe text input for a replay nonce")

// PrependFailureSampleNonce returns a modified request copy. It never mutates
// the persisted sample and only edits an existing user-visible text field.
func PrependFailureSampleNonce(body []byte, relayFormat types.RelayFormat, nonce string) ([]byte, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid failure sample JSON")
	}
	root := gjson.ParseBytes(body)
	var path string
	switch relayFormat {
	case types.RelayFormatOpenAI:
		path = findMessageTextPath(root.Get("messages"), "messages")
	case types.RelayFormatOpenAIResponses:
		input := root.Get("input")
		if input.Type == gjson.String {
			path = "input"
		} else {
			path = findMessageTextPath(input, "input")
		}
	case types.RelayFormatClaude:
		path = findMessageTextPath(root.Get("messages"), "messages")
	case types.RelayFormatGemini:
		path = findGeminiTextPath(root.Get("contents"))
	default:
		return nil, ErrNoSafeFailureSampleText
	}
	if path == "" {
		return nil, ErrNoSafeFailureSampleText
	}
	text := root.Get(path)
	workingCopy := append([]byte(nil), body...)
	updated, err := sjson.SetBytes(workingCopy, path, nonce+" "+text.String())
	if err != nil {
		return nil, fmt.Errorf("prepend failure sample nonce: %w", err)
	}
	return updated, nil
}

func findMessageTextPath(messages gjson.Result, basePath string) string {
	if !messages.IsArray() {
		return ""
	}
	items := messages.Array()
	for _, preferredRole := range []string{"user", "system", "assistant", ""} {
		for index, item := range items {
			role := item.Get("role").String()
			if role == "tool" || role == "function" || (preferredRole != "" && role != preferredRole) {
				continue
			}
			content := item.Get("content")
			if content.Type == gjson.String {
				return fmt.Sprintf("%s.%d.content", basePath, index)
			}
			if content.IsArray() {
				for contentIndex, part := range content.Array() {
					partType := part.Get("type").String()
					if (partType == "text" || partType == "input_text") && part.Get("text").Type == gjson.String {
						return fmt.Sprintf("%s.%d.content.%d.text", basePath, index, contentIndex)
					}
				}
			}
			if item.Get("type").String() == "input_text" && item.Get("text").Type == gjson.String {
				return fmt.Sprintf("%s.%d.text", basePath, index)
			}
		}
	}
	return ""
}

func findGeminiTextPath(contents gjson.Result) string {
	if !contents.IsArray() {
		return ""
	}
	items := contents.Array()
	for _, preferredRole := range []string{"user", ""} {
		for contentIndex, content := range items {
			role := content.Get("role").String()
			if preferredRole != "" && role != preferredRole {
				continue
			}
			parts := content.Get("parts")
			if !parts.IsArray() {
				continue
			}
			for partIndex, part := range parts.Array() {
				if part.Get("thought").Bool() || part.Get("text").Type != gjson.String {
					continue
				}
				return fmt.Sprintf("contents.%d.parts.%d.text", contentIndex, partIndex)
			}
		}
	}
	return ""
}
