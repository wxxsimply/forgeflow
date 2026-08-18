package tool

import (
	"context"
	"encoding/json"
	"time"

	"forgeflow/internal/domain"
	"forgeflow/internal/policy"
)

type Spec struct {
	Name           string           `json:"name"`
	Version        string           `json:"version"`
	Description    string           `json:"description"`
	InputSchema    json.RawMessage  `json:"inputSchema"`
	OutputSchema   json.RawMessage  `json:"outputSchema"`
	Risk           domain.RiskLevel `json:"risk"`
	Timeout        time.Duration    `json:"timeout"`
	MaxOutputBytes int64            `json:"maxOutputBytes"`
}

type CallContext struct {
	CallID       string
	NodeID       string
	RunID        string
	TraceID      string
	Agent        string
	Workspace    domain.WorkspaceRef
	Budget       domain.RunBudget
	Approval     *policy.ApprovalEvidence
	OnStarted    func()
	AllowedPaths []string
}

type Tool interface {
	Spec() Spec
	Analyze(json.RawMessage) (policy.Metadata, error)
	Execute(context.Context, CallContext, json.RawMessage) (json.RawMessage, error)
	ValidateOutput(json.RawMessage) error
}

type ApprovalRequirement struct {
	CallID        string
	ToolName      string
	ToolVersion   string
	InputSHA256   string
	WorkspaceID   string
	PolicyVersion string
	RuleID        string
	Reason        string
	Scope         []string
	Risk          domain.RiskLevel
}

type Invocation struct {
	Output   json.RawMessage
	Audit    domain.ToolCallAudit
	Approval *ApprovalRequirement
}
