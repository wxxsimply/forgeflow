package developer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

const maxImplementationOutputBytes = 1024 * 1024

var implementationResultSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "required":["summary","patch","changedFiles","evidence","unresolvedIssues","requestedApprovals"],
  "properties":{
    "summary":{"type":"string","minLength":1,"maxLength":4000},
    "patch":{"type":"string","minLength":1,"maxLength":196608},
    "changedFiles":{"type":"array","minItems":1,"maxItems":200,"items":{"type":"string","minLength":1}},
    "evidence":{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,"maxLength":4000}},
    "unresolvedIssues":{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,"maxLength":4000}},
    "requestedApprovals":{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,"maxLength":4000}}
  }
}`)

func ImplementationResultSchema() json.RawMessage {
	return append(json.RawMessage(nil), implementationResultSchema...)
}

type strictImplementation struct {
	Summary            *string   `json:"summary"`
	Patch              *string   `json:"patch"`
	ChangedFiles       *[]string `json:"changedFiles"`
	Evidence           *[]string `json:"evidence"`
	UnresolvedIssues   *[]string `json:"unresolvedIssues"`
	RequestedApprovals *[]string `json:"requestedApprovals"`
}

func DecodeImplementationResult(output string) (domain.ImplementationResult, error) {
	if strings.TrimSpace(output) == "" || len(output) > maxImplementationOutputBytes {
		return domain.ImplementationResult{}, apperror.New(apperror.CodeModelOutput, "developer returned empty or oversized output")
	}
	normalized, err := normalizeImplementationJSON(output)
	if err != nil {
		return domain.ImplementationResult{}, apperror.Wrap(err, apperror.CodeModelOutput, "developer.decode", "developer output contains an invalid JSON fence")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(normalized))
	decoder.DisallowUnknownFields()
	var decoded strictImplementation
	if err := decoder.Decode(&decoded); err != nil {
		return domain.ImplementationResult{}, apperror.Wrap(err, apperror.CodeModelOutput, "developer.decode", "developer output is not strict JSON")
	}
	if err := requireJSONEOF(decoder); err != nil {
		return domain.ImplementationResult{}, apperror.Wrap(err, apperror.CodeModelOutput, "developer.decode", "developer output contains trailing data")
	}
	if decoded.Summary == nil || decoded.Patch == nil || decoded.ChangedFiles == nil || decoded.Evidence == nil || decoded.UnresolvedIssues == nil || decoded.RequestedApprovals == nil {
		return domain.ImplementationResult{}, apperror.New(apperror.CodeModelOutput, "developer output is missing a required field")
	}
	result := domain.ImplementationResult{
		Summary: *decoded.Summary, Patch: *decoded.Patch, ChangedFiles: *decoded.ChangedFiles,
		Evidence: *decoded.Evidence, UnresolvedIssues: *decoded.UnresolvedIssues,
		RequestedApprovals: *decoded.RequestedApprovals,
	}
	if err := result.Validate(); err != nil {
		return domain.ImplementationResult{}, apperror.Wrap(err, apperror.CodeModelOutput, "developer.validate", "developer output failed domain validation")
	}
	return result, nil
}

func normalizeImplementationJSON(output string) (string, error) {
	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed, nil
	}
	firstNewline := strings.IndexByte(trimmed, '\n')
	if firstNewline < 0 || !strings.EqualFold(strings.TrimSpace(trimmed[:firstNewline]), "```json") {
		return "", fmt.Errorf("opening fence must be exactly ```json")
	}
	bodyAndFence := trimmed[firstNewline+1:]
	lastNewline := strings.LastIndexByte(bodyAndFence, '\n')
	if lastNewline < 0 || strings.TrimSpace(bodyAndFence[lastNewline+1:]) != "```" {
		return "", fmt.Errorf("closing fence must be the final line")
	}
	body := strings.TrimSpace(bodyAndFence[:lastNewline])
	if body == "" {
		return "", fmt.Errorf("JSON fence is empty")
	}
	return body, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("unexpected second JSON value")
	}
	return err
}
