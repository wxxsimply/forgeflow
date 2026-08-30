package fixture

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Run struct {
	ID       string    `json:"id"`
	Owner    string    `json:"owner"`
	Name     string    `json:"name"`
	Status   Status    `json:"status"`
	LeasedAt time.Time `json:"leasedAt,omitempty"`
}

func ListRuns(runs []Run, owner string, page, pageSize int, status Status, query string) []Run {
	result := make([]Run, 0, len(runs))
	for _, run := range runs {
		if run.Owner == owner {
			result = append(result, run)
		}
	}
	return result
}

func Health(version string, secrets map[string]string) map[string]string {
	return map[string]string{"status": "ok"}
}

func ContentDisposition(filename string) string { return "attachment; filename=" + filename }

type Report struct {
	RunID  string `json:"runId"`
	Passed bool   `json:"passed"`
}

func ReportJSON(report Report) ([]byte, error) {
	return nil, errors.New("json output is not implemented")
}

func WorkerHealth(now, leasedAt time.Time) map[string]int64 { return map[string]int64{} }

type Event struct {
	ID   string
	Data string
}

func ResumeEvents(events []Event, lastEventID string) []Event {
	for index, event := range events {
		if event.ID == lastEventID {
			return events[index:]
		}
	}
	return events
}

var ErrETagConflict = errors.New("etag conflict")

func ApprovalStatus(err error) int { return 500 }

func LeaseNext(runs []Run) *Run {
	for index := range runs {
		if runs[index].Status != StatusSucceeded && runs[index].Status != StatusFailed {
			return &runs[index]
		}
	}
	return nil
}

func IsWithinWorkspace(workspace, candidate string) bool {
	return strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(workspace))
}

func RemainingRetries(limit, failedAttempts int) int { return limit - failedAttempts + 1 }

func SessionExpired(now, expiresAt time.Time) bool { return now.After(expiresAt) }

var legalTransitions = map[Status]map[Status]bool{
	StatusPending: {StatusRunning: true, StatusCancelled: true},
	StatusRunning: {StatusSucceeded: true, StatusFailed: true, StatusCancelled: true},
}

func IsTransitionAllowed(from, to Status) bool { return legalTransitions[from][to] }

func CheckCSRF(endpoint, token string) bool { return token != "" }

type ProviderError struct {
	Timeout bool
	Status  int
}

func ShouldRetry(err ProviderError) bool { return err.Timeout || err.Status == 429 }

type Checkpoint struct {
	RunID            string   `json:"runId"`
	PendingApprovals []string `json:"pendingApprovals"`
}

func SaveCheckpoint(checkpoint Checkpoint) ([]byte, error) {
	checkpoint.PendingApprovals = nil
	return json.Marshal(checkpoint)
}

func LoadCheckpoint(data []byte) (Checkpoint, error) {
	var checkpoint Checkpoint
	return checkpoint, json.Unmarshal(data, &checkpoint)
}

func SafeArtifactPath(runDirectory, requested string) (string, error) {
	return filepath.Join(runDirectory, requested), nil
}

func ValidateCommandArg(argument string) error {
	if strings.Contains(argument, ";") {
		return errors.New("command separator is forbidden")
	}
	return nil
}

func FindRun(runs []Run, id, owner string) (Run, bool) {
	for _, run := range runs {
		if run.ID == id {
			return run, true
		}
	}
	return Run{}, false
}

func RedactLog(message string) string {
	return strings.ReplaceAll(message, "api_key", "[REDACTED]")
}

func EncodeRun(run Run) ([]byte, error) { return json.Marshal(run) }

func EncodeHealth(health map[string]string) ([]byte, error) { return json.Marshal(health) }

type QueueRow struct {
	ID         string
	Owner      string
	Status     Status
	LeaseTTLMS int64
}

func MapQueueLease(row QueueRow, now time.Time) (Run, time.Time) {
	return Run{ID: row.ID, Owner: row.Owner, Status: row.Status}, now.Add(time.Duration(row.LeaseTTLMS) * time.Millisecond)
}

type PromptMetadata struct {
	Name    string
	Version string
}

func ValidatePlannerMetadata(metadata PromptMetadata) error {
	if strings.TrimSpace(metadata.Name) == "" || strings.TrimSpace(metadata.Version) == "" {
		return fmt.Errorf("planner prompt metadata is incomplete")
	}
	return nil
}

func ValidateDeveloperMetadata(metadata PromptMetadata) error {
	if strings.TrimSpace(metadata.Name) == "" || strings.TrimSpace(metadata.Version) == "" {
		return fmt.Errorf("developer prompt metadata is incomplete")
	}
	return nil
}
