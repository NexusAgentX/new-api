package service

import (
	"encoding/hex"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	channelAffinityMessageHashSource  = "message_hash"
	channelAffinityMessageHashVersion = "v1"
	ginKeyChannelAffinityMessageHash  = "channel_affinity_message_hash"
)

type channelAffinityMessageHashes struct {
	Protocol string
	Values   []string
}

func extractChannelAffinityMessageHashes(c *gin.Context) (channelAffinityMessageHashes, bool) {
	if c == nil {
		return channelAffinityMessageHashes{}, false
	}
	if cached, ok := c.Get(ginKeyChannelAffinityMessageHash); ok {
		hashes, valid := cached.(channelAffinityMessageHashes)
		return hashes, valid && len(hashes.Values) > 0
	}

	path := ""
	if c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	protocol := channelAffinityMessageProtocol(path)
	if protocol == "" {
		c.Set(ginKeyChannelAffinityMessageHash, channelAffinityMessageHashes{})
		return channelAffinityMessageHashes{}, false
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		c.Set(ginKeyChannelAffinityMessageHash, channelAffinityMessageHashes{})
		return channelAffinityMessageHashes{}, false
	}
	body, err := storage.Bytes()
	if err != nil || len(body) == 0 {
		c.Set(ginKeyChannelAffinityMessageHash, channelAffinityMessageHashes{})
		return channelAffinityMessageHashes{}, false
	}

	var root map[string]any
	if err := common.Unmarshal(body, &root); err != nil {
		c.Set(ginKeyChannelAffinityMessageHash, channelAffinityMessageHashes{})
		return channelAffinityMessageHashes{}, false
	}

	system := make([]any, 0)
	var firstUser any
	var firstAssistant any
	foundUser := false
	foundAssistant := false

	switch protocol {
	case "openai_chat":
		messages, _ := channelAffinityArray(root["messages"])
		for _, item := range messages {
			message, ok := channelAffinityObject(item)
			if !ok {
				continue
			}
			role := channelAffinityMessageRole(message["role"])
			switch role {
			case "system", "developer":
				system = append(system, normalizeChannelAffinityMessage(message, role, false))
			case "user":
				if !foundUser {
					firstUser = normalizeChannelAffinityMessage(message, "user", false)
					foundUser = true
				}
			case "assistant":
				if !foundAssistant {
					firstAssistant = normalizeChannelAffinityMessage(message, "assistant", false)
					foundAssistant = true
				}
			}
		}
	case "openai_responses":
		if instructions, ok := root["instructions"]; ok && instructions != nil {
			system = append(system, map[string]any{
				"role":    "system",
				"content": instructions,
			})
		}
		input := root["input"]
		if inputText, ok := input.(string); ok {
			firstUser = map[string]any{
				"role":    "user",
				"content": inputText,
			}
			foundUser = true
		} else if items, ok := channelAffinityArray(input); ok {
			for _, item := range items {
				message, isObject := channelAffinityObject(item)
				if !isObject {
					continue
				}
				role := channelAffinityMessageRole(message["role"])
				switch role {
				case "system", "developer":
					system = append(system, normalizeChannelAffinityMessage(message, role, true))
				case "user":
					if !foundUser {
						firstUser = normalizeChannelAffinityMessage(message, "user", true)
						foundUser = true
					}
				case "assistant":
					if !foundAssistant {
						firstAssistant = normalizeChannelAffinityMessage(message, "assistant", true)
						foundAssistant = true
					}
				default:
					if !foundUser && strings.HasPrefix(strings.ToLower(channelAffinityString(message["type"])), "input_") {
						firstUser = map[string]any{
							"role":    "user",
							"content": message,
						}
						foundUser = true
					}
				}
			}
		}
	case "anthropic_messages":
		if systemValue, ok := root["system"]; ok && systemValue != nil {
			system = append(system, map[string]any{
				"role":    "system",
				"content": systemValue,
			})
		}
		messages, _ := channelAffinityArray(root["messages"])
		for _, item := range messages {
			message, ok := channelAffinityObject(item)
			if !ok {
				continue
			}
			role := channelAffinityMessageRole(message["role"])
			switch role {
			case "system", "developer":
				system = append(system, normalizeChannelAffinityMessage(message, role, false))
			case "user":
				if !foundUser {
					firstUser = normalizeChannelAffinityMessage(message, "user", false)
					foundUser = true
				}
			case "assistant":
				if !foundAssistant {
					firstAssistant = normalizeChannelAffinityMessage(message, "assistant", false)
					foundAssistant = true
				}
			}
		}
	case "gemini":
		if systemValue, ok := channelAffinityFirstValue(root, "systemInstruction", "system_instruction"); ok {
			if systemObject, isObject := channelAffinityObject(systemValue); isObject {
				system = append(system, map[string]any{
					"role":  "system",
					"parts": systemObject["parts"],
				})
			} else {
				system = append(system, map[string]any{
					"role":    "system",
					"content": systemValue,
				})
			}
		}
		contents, _ := channelAffinityArray(root["contents"])
		for _, item := range contents {
			content, ok := channelAffinityObject(item)
			if !ok {
				continue
			}
			role := channelAffinityMessageRole(content["role"])
			if role == "" {
				role = "user"
			}
			normalized := map[string]any{
				"role":  role,
				"parts": content["parts"],
			}
			switch role {
			case "user":
				if !foundUser {
					firstUser = normalized
					foundUser = true
				}
			case "model", "assistant":
				if !foundAssistant {
					normalized["role"] = "assistant"
					firstAssistant = normalized
					foundAssistant = true
				}
			}
		}
	}

	if !foundUser {
		c.Set(ginKeyChannelAffinityMessageHash, channelAffinityMessageHashes{})
		return channelAffinityMessageHashes{}, false
	}

	initial, ok := buildChannelAffinityMessageHash(protocol, system, firstUser, nil)
	if !ok {
		c.Set(ginKeyChannelAffinityMessageHash, channelAffinityMessageHashes{})
		return channelAffinityMessageHashes{}, false
	}

	hashes := channelAffinityMessageHashes{
		Protocol: protocol,
		Values:   []string{initial},
	}
	if foundAssistant {
		if extended, extendedOK := buildChannelAffinityMessageHash(protocol, system, firstUser, firstAssistant); extendedOK {
			hashes.Values = []string{extended, initial}
		}
	}
	c.Set(ginKeyChannelAffinityMessageHash, hashes)
	return hashes, true
}

func channelAffinityMessageProtocol(path string) string {
	switch {
	case strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/"):
		return "gemini"
	case strings.Contains(path, "/messages"):
		return "anthropic_messages"
	case strings.Contains(path, "/responses"):
		return "openai_responses"
	case strings.Contains(path, "/chat/completions"):
		return "openai_chat"
	default:
		return ""
	}
}

func buildChannelAffinityMessageHash(protocol string, system []any, firstUser, firstAssistant any) (string, bool) {
	anchor := map[string]any{
		"version":    channelAffinityMessageHashVersion,
		"protocol":   protocol,
		"system":     system,
		"first_user": firstUser,
	}
	if firstAssistant != nil {
		anchor["first_assistant"] = firstAssistant
	}
	canonical, err := common.Marshal(anchor)
	if err != nil {
		return "", false
	}
	if strings.TrimSpace(common.CryptoSecret) != "" {
		return common.HmacSha256(string(canonical), common.CryptoSecret), true
	}
	return hex.EncodeToString(common.Sha256Raw(canonical)), true
}

func normalizeChannelAffinityMessage(message map[string]any, role string, includeType bool) map[string]any {
	normalized := map[string]any{
		"role": role,
	}
	if includeType {
		if messageType, ok := message["type"]; ok {
			normalized["type"] = messageType
		}
	}
	if content, ok := message["content"]; ok {
		normalized["content"] = content
	}
	if name, ok := message["name"]; ok {
		normalized["name"] = name
	}
	if role == "assistant" {
		for _, key := range []string{"tool_calls", "function_call"} {
			if value, ok := message[key]; ok {
				normalized[key] = value
			}
		}
	}
	return normalized
}

func channelAffinityMessageRole(value any) string {
	return strings.ToLower(strings.TrimSpace(channelAffinityString(value)))
}

func channelAffinityString(value any) string {
	stringValue, ok := value.(string)
	if !ok {
		return ""
	}
	return stringValue
}

func channelAffinityObject(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func channelAffinityArray(value any) ([]any, bool) {
	array, ok := value.([]any)
	return array, ok
}

func channelAffinityFirstValue(object map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		value, ok := object[key]
		if ok && value != nil {
			return value, true
		}
	}
	return nil, false
}
