package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"forgeflow/internal/domain"
)

const SchemaVersion = "forgeflow.audit/v1"

type Event struct {
	SchemaVersion string         `json:"schemaVersion"`
	ID            string         `json:"id"`
	OccurredAt    time.Time      `json:"occurredAt"`
	ActorID       string         `json:"actorId,omitempty"`
	Action        string         `json:"action"`
	ResourceType  string         `json:"resourceType"`
	ResourceID    string         `json:"resourceId"`
	RequestID     string         `json:"requestId"`
	SourceIPHash  string         `json:"sourceIpHash,omitempty"`
	Details       map[string]any `json:"details"`
	Integrity     string         `json:"integrity"`
}

type Appender interface {
	Append(context.Context, Event) error
}

type RecoveryPage struct {
	Events     []Event
	NextCursor string
}

func NewEvent(actorID, action, resourceType, resourceID, requestID, sourceIP string, details map[string]any, integrityKey []byte, now time.Time) (Event, error) {
	if len(integrityKey) != 32 {
		return Event{}, fmt.Errorf("audit integrity key must contain exactly 32 bytes")
	}
	if !safeValue(action, 128) || !safeValue(resourceType, 64) || !safeValue(resourceID, 256) || !safeValue(requestID, 128) {
		return Event{}, fmt.Errorf("audit event identity fields are invalid")
	}
	event := Event{
		SchemaVersion: SchemaVersion,
		ID:            domain.NewID(),
		OccurredAt:    now.UTC(),
		ActorID:       bounded(actorID, 128),
		Action:        action,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		RequestID:     requestID,
		SourceIPHash:  digest(integrityKey, "source-ip:"+strings.TrimSpace(sourceIP)),
		Details:       sanitizeMap(details, 0),
	}
	if strings.TrimSpace(sourceIP) == "" {
		event.SourceIPHash = ""
	}
	signature, err := sign(event, integrityKey)
	if err != nil {
		return Event{}, err
	}
	event.Integrity = signature
	return event, nil
}

func Verify(event Event, integrityKey []byte) error {
	if event.SchemaVersion != SchemaVersion || event.ID == "" || event.OccurredAt.IsZero() || event.Integrity == "" {
		return fmt.Errorf("audit event envelope is invalid")
	}
	want, err := sign(event, integrityKey)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(want), []byte(event.Integrity)) {
		return fmt.Errorf("audit event integrity verification failed")
	}
	return nil
}

func sign(event Event, key []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("audit integrity key must contain exactly 32 bytes")
	}
	event.Integrity = ""
	data, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("marshal audit event for signing: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("forgeflow:audit:v1:"))
	_, _ = mac.Write(data)
	return "hmac-sha256:" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func digest(key []byte, value string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("forgeflow:audit:" + value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func sanitizeMap(value map[string]any, depth int) map[string]any {
	result := map[string]any{}
	if depth > 3 {
		return result
	}
	count := 0
	for key, item := range value {
		if count >= 32 || !safeDetailKey(key) || sensitiveKey(key) {
			continue
		}
		if sanitized, ok := sanitizeValue(item, depth+1); ok {
			result[key] = sanitized
			count++
		}
	}
	return result
}

func sanitizeValue(value any, depth int) (any, bool) {
	switch typed := value.(type) {
	case nil, bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return typed, true
	case string:
		return bounded(typed, 256), true
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano), true
	case map[string]any:
		return sanitizeMap(typed, depth), true
	case []string:
		limit := min(len(typed), 20)
		items := make([]string, limit)
		for index := range limit {
			items[index] = bounded(typed[index], 256)
		}
		return items, true
	default:
		return nil, false
	}
}

func bounded(value string, limit int) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func safeValue(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

var detailKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

func safeDetailKey(value string) bool { return detailKeyPattern.MatchString(value) }

func sensitiveKey(value string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "-", "_"), ".", "_"))
	for _, token := range []string{"authorization", "cookie", "password", "secret", "token", "api_key", "apikey", "dsn", "prompt", "patch", "source", "content", "body", "task", "recovery", "mfa_code"} {
		if normalized == token || strings.HasSuffix(normalized, "_"+token) || strings.HasPrefix(normalized, token+"_") {
			return true
		}
	}
	return false
}
