package tool

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"forgeflow/internal/apperror"
)

func redactOutput(output json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, apperror.Wrap(err, apperror.CodeInternal, "tool.output.decode", "tool returned malformed JSON")
	}
	redacted := redactValue(value, "")
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return nil, apperror.Wrap(err, apperror.CodeInternal, "tool.output.redact", "tool output could not be redacted")
	}
	return encoded, nil
}

func redactValue(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for childKey, child := range typed {
			result[childKey] = redactValue(child, childKey)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = redactValue(child, key)
		}
		return result
	case string:
		if sensitiveKey(key) {
			return "[REDACTED]"
		}
		return redactKnownToken(typed)
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
	for _, fragment := range []string{"password", "secret", "token", "apikey", "credential", "privatekey", "cookie"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func redactKnownToken(value string) string {
	return knownTokenPattern.ReplaceAllString(value, "[REDACTED]")
}

var knownTokenPattern = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{9,}\b`)
