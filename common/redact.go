package common

import "strings"

const RedactedSensitiveValue = "[REDACTED]"

// RedactExactValue replaces only the exact request-scoped value supplied by
// the caller. It intentionally does not attempt to recognize secret patterns.
func RedactExactValue(text string, value string) string {
	if value == "" {
		return text
	}
	return strings.ReplaceAll(text, value, RedactedSensitiveValue)
}
