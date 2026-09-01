package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type RunStatus string

const (
	StatusCreated               RunStatus = "created"
	StatusPlanning              RunStatus = "planning"
	StatusWaitingPlanApproval   RunStatus = "waiting_for_plan_approval"
	StatusPreparingWorkspace    RunStatus = "preparing_workspace"
	StatusImplementing          RunStatus = "implementing"
	StatusEvaluating            RunStatus = "evaluating"
	StatusWaitingActionApproval RunStatus = "waiting_for_action_approval"
	StatusPaused                RunStatus = "paused"
	StatusRepairing             RunStatus = "repairing"
	StatusCompleted             RunStatus = "completed"
	StatusFailed                RunStatus = "failed"
	StatusCancelled             RunStatus = "cancelled"
)

func (s RunStatus) Terminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled
}

type EventType string

const (
	EventRunCreated        EventType = "run_created"
	EventNodeStarted       EventType = "node_started"
	EventNodeCompleted     EventType = "node_completed"
	EventNodeInterrupted   EventType = "node_interrupted"
	EventNodeFailed        EventType = "node_failed"
	EventApprovalRequested EventType = "approval_requested"
	EventApprovalResolved  EventType = "approval_resolved"
	EventStatusChanged     EventType = "status_changed"
	EventNodeRetrying      EventType = "node_retrying"
	EventNodeReused        EventType = "node_reused"
	EventBudgetExhausted   EventType = "budget_exhausted"
	EventCancellationAsked EventType = "cancellation_requested"
	EventRunCancelled      EventType = "run_cancelled"
	EventPauseRequested    EventType = "pause_requested"
	EventRunPaused         EventType = "run_paused"
	EventRunResumed        EventType = "run_resumed"
	EventParallelCompleted EventType = "parallel_completed"
	EventToolCallStarted   EventType = "tool_call_started"
	EventToolCallCompleted EventType = "tool_call_completed"
	EventToolCallDenied    EventType = "tool_call_denied"
	EventToolCallFailed    EventType = "tool_call_failed"
)

type RunEvent struct {
	EventID   string    `json:"eventId"`
	RunID     string    `json:"runId"`
	TraceID   string    `json:"traceId"`
	Type      EventType `json:"type"`
	NodeID    string    `json:"nodeId,omitempty"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
)

type ApprovalRequest struct {
	ApprovalID    string         `json:"approvalId"`
	RunID         string         `json:"runId"`
	ActionType    string         `json:"actionType"`
	Reason        string         `json:"reason"`
	Scope         []string       `json:"scope"`
	Risk          RiskLevel      `json:"risk"`
	Status        ApprovalStatus `json:"status"`
	Comment       string         `json:"comment,omitempty"`
	RequestedAt   time.Time      `json:"requestedAt"`
	ResolvedAt    *time.Time     `json:"resolvedAt,omitempty"`
	ToolCallID    string         `json:"toolCallId,omitempty"`
	ToolName      string         `json:"toolName,omitempty"`
	ToolVersion   string         `json:"toolVersion,omitempty"`
	InputSHA256   string         `json:"inputSha256,omitempty"`
	WorkspaceID   string         `json:"workspaceId,omitempty"`
	PolicyVersion string         `json:"policyVersion,omitempty"`
}

type RunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type NodeExecutionStatus string

const (
	NodeExecutionRunning     NodeExecutionStatus = "running"
	NodeExecutionSucceeded   NodeExecutionStatus = "succeeded"
	NodeExecutionInterrupted NodeExecutionStatus = "interrupted"
	NodeExecutionFailed      NodeExecutionStatus = "failed"
)

type NodeExecution struct {
	Key            string              `json:"key"`
	NodeID         string              `json:"nodeId"`
	Iteration      int                 `json:"iteration"`
	IdempotencyKey string              `json:"idempotencyKey"`
	Status         NodeExecutionStatus `json:"status"`
	Attempts       int                 `json:"attempts"`
	StartedAt      time.Time           `json:"startedAt"`
	CompletedAt    *time.Time          `json:"completedAt,omitempty"`
	LastError      string              `json:"lastError,omitempty"`
}

type BranchStatus string

const (
	BranchPending   BranchStatus = "pending"
	BranchRunning   BranchStatus = "running"
	BranchSucceeded BranchStatus = "succeeded"
	BranchFailed    BranchStatus = "failed"
	BranchCancelled BranchStatus = "cancelled"
)

func (s BranchStatus) Terminal() bool {
	return s == BranchSucceeded || s == BranchFailed || s == BranchCancelled
}

type BranchState struct {
	ID          string          `json:"id"`
	Status      BranchStatus    `json:"status"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	StartedAt   time.Time       `json:"startedAt"`
	CompletedAt *time.Time      `json:"completedAt,omitempty"`
}

type CancellationState struct {
	RequestedAt *time.Time `json:"requestedAt,omitempty"`
	RequestedBy string     `json:"requestedBy,omitempty"`
	Reason      string     `json:"reason,omitempty"`
}

type PauseState struct {
	RequestedAt    *time.Time `json:"requestedAt,omitempty"`
	RequestedBy    string     `json:"requestedBy,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	PausedAt       *time.Time `json:"pausedAt,omitempty"`
	PreviousStatus RunStatus  `json:"previousStatus,omitempty"`
}

func (p PauseState) Requested() bool { return p.RequestedAt != nil }

type ResumeGuard struct {
	WorkspaceID      string    `json:"workspaceId,omitempty"`
	WorkspacePath    string    `json:"workspacePath,omitempty"`
	BaseCommit       string    `json:"baseCommit,omitempty"`
	PolicyVersions   []string  `json:"policyVersions"`
	PromptBindings   []string  `json:"promptBindings"`
	ModelBindings    []string  `json:"modelBindings"`
	ToolBindings     []string  `json:"toolBindings"`
	ApprovalID       string    `json:"approvalId,omitempty"`
	ApprovalInputSHA string    `json:"approvalInputSha,omitempty"`
	ApprovalPolicy   string    `json:"approvalPolicy,omitempty"`
	CapturedAt       time.Time `json:"capturedAt"`
}

func (c CancellationState) Requested() bool {
	return c.RequestedAt != nil
}

type RunBudget struct {
	MaxIterations       int           `json:"maxIterations"`
	MaxNodeCalls        int           `json:"maxNodeCalls"`
	MaxModelCalls       int           `json:"maxModelCalls"`
	MaxToolCalls        int           `json:"maxToolCalls"`
	MaxToolOutputBytes  int64         `json:"maxToolOutputBytes"`
	MaxChangedFiles     int           `json:"maxChangedFiles"`
	MaxDiffBytes        int64         `json:"maxDiffBytes"`
	MaxDiffLines        int           `json:"maxDiffLines"`
	MaxRepairs          int           `json:"maxRepairs"`
	MaxInputTokens      int           `json:"maxInputTokens"`
	MaxOutputTokens     int           `json:"maxOutputTokens"`
	MaxEstimatedCostUSD float64       `json:"maxEstimatedCostUsd"`
	MaxDuration         time.Duration `json:"maxDuration"`
	NodeCalls           int           `json:"nodeCalls"`
	ModelCalls          int           `json:"modelCalls"`
	ToolCalls           int           `json:"toolCalls"`
	ToolOutputBytes     int64         `json:"toolOutputBytes"`
	InputTokens         int           `json:"inputTokens"`
	OutputTokens        int           `json:"outputTokens"`
	EstimatedCostUSD    float64       `json:"estimatedCostUsd"`
}

func DefaultRunBudget(maxIterations int) RunBudget {
	if maxIterations <= 0 {
		maxIterations = 2
	}
	return RunBudget{
		MaxIterations:       maxIterations,
		MaxNodeCalls:        100,
		MaxModelCalls:       20,
		MaxToolCalls:        200,
		MaxToolOutputBytes:  16 * 1024 * 1024,
		MaxChangedFiles:     32,
		MaxDiffBytes:        1024 * 1024,
		MaxDiffLines:        4_000,
		MaxRepairs:          1,
		MaxInputTokens:      200_000,
		MaxOutputTokens:     40_000,
		MaxEstimatedCostUSD: 10,
		MaxDuration:         30 * time.Minute,
	}
}

func (b RunBudget) ToolCallAllowed() (bool, string) {
	if b.MaxToolCalls > 0 && b.ToolCalls >= b.MaxToolCalls {
		return false, "tool call budget exhausted"
	}
	if b.MaxToolOutputBytes > 0 && b.ToolOutputBytes >= b.MaxToolOutputBytes {
		return false, "tool output byte budget exhausted"
	}
	return true, ""
}

func (b RunBudget) ModelCallAllowed() (bool, string) {
	if b.MaxModelCalls > 0 && b.ModelCalls >= b.MaxModelCalls {
		return false, "model call budget exhausted"
	}
	if b.MaxInputTokens > 0 && b.InputTokens >= b.MaxInputTokens {
		return false, "model input token budget exhausted"
	}
	if b.MaxOutputTokens > 0 && b.OutputTokens >= b.MaxOutputTokens {
		return false, "model output token budget exhausted"
	}
	if b.MaxEstimatedCostUSD > 0 && b.EstimatedCostUSD >= b.MaxEstimatedCostUSD {
		return false, "model cost budget exhausted"
	}
	return true, ""
}

func (b RunBudget) ModelUsageAllowed() (bool, string) {
	if b.MaxInputTokens > 0 && b.InputTokens > b.MaxInputTokens {
		return false, "model input token budget exceeded"
	}
	if b.MaxOutputTokens > 0 && b.OutputTokens > b.MaxOutputTokens {
		return false, "model output token budget exceeded"
	}
	if b.MaxEstimatedCostUSD > 0 && b.EstimatedCostUSD > b.MaxEstimatedCostUSD {
		return false, "model cost budget exceeded"
	}
	return true, ""
}

type ModelInvocation struct {
	Agent               string    `json:"agent,omitempty"`
	AgentVersion        string    `json:"agentVersion,omitempty"`
	NodeID              string    `json:"nodeId,omitempty"`
	Provider            string    `json:"provider"`
	Model               string    `json:"model"`
	ResponseID          string    `json:"responseId,omitempty"`
	PromptVersion       string    `json:"promptVersion"`
	PromptSHA256        string    `json:"promptSha256"`
	Status              string    `json:"status"`
	InputTokens         int       `json:"inputTokens"`
	OutputTokens        int       `json:"outputTokens"`
	CachedInputTokens   int       `json:"cachedInputTokens"`
	ReasoningTokens     int       `json:"reasoningTokens"`
	LatencyMilliseconds int64     `json:"latencyMilliseconds"`
	EstimatedCostUSD    float64   `json:"estimatedCostUsd"`
	PricingConfigured   bool      `json:"pricingConfigured"`
	CreatedAt           time.Time `json:"createdAt"`
}

type ToolCallStatus string

const (
	ToolCallApprovalRequired ToolCallStatus = "approval_required"
	ToolCallSucceeded        ToolCallStatus = "succeeded"
	ToolCallFailed           ToolCallStatus = "failed"
	ToolCallDenied           ToolCallStatus = "denied"
)

type ToolCallAudit struct {
	CallID              string         `json:"callId"`
	NodeID              string         `json:"nodeId,omitempty"`
	Agent               string         `json:"agent"`
	ToolName            string         `json:"toolName"`
	ToolVersion         string         `json:"toolVersion"`
	InputSHA256         string         `json:"inputSha256"`
	WorkspaceID         string         `json:"workspaceId"`
	PolicyVersion       string         `json:"policyVersion"`
	PolicyRuleID        string         `json:"policyRuleId"`
	PolicyAction        string         `json:"policyAction"`
	Status              ToolCallStatus `json:"status"`
	OutputBytes         int64          `json:"outputBytes"`
	OutputTruncated     bool           `json:"outputTruncated"`
	LatencyMilliseconds int64          `json:"latencyMilliseconds"`
	ErrorCode           string         `json:"errorCode,omitempty"`
	CreatedAt           time.Time      `json:"createdAt"`
	CompletedAt         *time.Time     `json:"completedAt,omitempty"`
}

func (b RunBudget) NodeCallAllowed(startedAt, now time.Time, iteration int) (bool, string) {
	if b.MaxIterations > 0 && iteration >= b.MaxIterations {
		return false, "iteration budget exhausted"
	}
	if b.MaxNodeCalls > 0 && b.NodeCalls >= b.MaxNodeCalls {
		return false, "node call budget exhausted"
	}
	if b.MaxDuration > 0 && now.Sub(startedAt) >= b.MaxDuration {
		return false, "run duration budget exhausted"
	}
	return true, ""
}

type RunState struct {
	RunID            string                   `json:"runId"`
	OwnerID          string                   `json:"ownerId,omitempty"`
	RepositoryID     string                   `json:"repositoryId,omitempty"`
	TraceID          string                   `json:"traceId"`
	Version          int64                    `json:"version"`
	Status           RunStatus                `json:"status"`
	Task             string                   `json:"task"`
	RepositoryPath   string                   `json:"repositoryPath"`
	BaseRevision     string                   `json:"baseRevision"`
	CurrentNodeID    string                   `json:"currentNodeId"`
	CompletedNodeIDs []string                 `json:"completedNodeIds"`
	NodeExecutions   map[string]NodeExecution `json:"nodeExecutions"`
	PendingBranches  map[string]BranchState   `json:"pendingBranches"`
	Plan             *ExecutionPlan           `json:"plan,omitempty"`
	Implementation   *ImplementationResult    `json:"implementation,omitempty"`
	Diff             *DiffArtifact            `json:"diff,omitempty"`
	TestCommand      *TestCommand             `json:"testCommand,omitempty"`
	TestAssessment   *TestAssessment          `json:"testAssessment,omitempty"`
	ReviewResult     *ReviewResult            `json:"reviewResult,omitempty"`
	SecurityResult   *SecurityResult          `json:"securityResult,omitempty"`
	AssessmentErrors map[string]string        `json:"assessmentErrors"`
	JudgeDecision    *JudgeDecision           `json:"judgeDecision,omitempty"`
	JudgeDecisions   []JudgeDecision          `json:"judgeDecisions"`
	RepairCount      int                      `json:"repairCount"`
	ModelInvocations []ModelInvocation        `json:"modelInvocations"`
	ToolCallAudits   []ToolCallAudit          `json:"toolCallAudits"`
	Workspace        *WorkspaceRef            `json:"workspace,omitempty"`
	Iteration        int                      `json:"iteration"`
	MaxIterations    int                      `json:"maxIterations"`
	Budget           RunBudget                `json:"budget"`
	Cancellation     CancellationState        `json:"cancellation"`
	Pause            PauseState               `json:"pause"`
	ResumeGuard      *ResumeGuard             `json:"resumeGuard,omitempty"`
	ChangedFiles     []string                 `json:"changedFiles"`
	PendingApproval  *ApprovalRequest         `json:"pendingApproval,omitempty"`
	Error            *RunError                `json:"error,omitempty"`
	CreatedAt        time.Time                `json:"createdAt"`
	UpdatedAt        time.Time                `json:"updatedAt"`
	Events           []RunEvent               `json:"events"`
}

type NewRunInput struct {
	OwnerID        string
	RepositoryID   string
	Task           string
	RepositoryPath string
	BaseRevision   string
	MaxIterations  int
	Budget         *RunBudget
}

func NewRunState(input NewRunInput) *RunState {
	now := time.Now().UTC()
	if input.BaseRevision == "" {
		input.BaseRevision = "HEAD"
	}
	if input.MaxIterations == 0 {
		input.MaxIterations = 2
	}
	budget := DefaultRunBudget(input.MaxIterations)
	if input.Budget != nil {
		budget = *input.Budget
		if budget.MaxIterations <= 0 {
			budget.MaxIterations = input.MaxIterations
		}
	}
	state := &RunState{
		RunID:            NewID(),
		OwnerID:          input.OwnerID,
		RepositoryID:     input.RepositoryID,
		TraceID:          NewID(),
		Status:           StatusCreated,
		Task:             input.Task,
		RepositoryPath:   input.RepositoryPath,
		BaseRevision:     input.BaseRevision,
		CurrentNodeID:    "start",
		CompletedNodeIDs: []string{},
		NodeExecutions:   map[string]NodeExecution{},
		PendingBranches:  map[string]BranchState{},
		ModelInvocations: []ModelInvocation{},
		ToolCallAudits:   []ToolCallAudit{},
		AssessmentErrors: map[string]string{},
		JudgeDecisions:   []JudgeDecision{},
		MaxIterations:    input.MaxIterations,
		Budget:           budget,
		ChangedFiles:     []string{},
		CreatedAt:        now,
		UpdatedAt:        now,
		Events:           []RunEvent{},
	}
	state.AppendEvent(EventRunCreated, "", "Run created")
	return state
}

func (s *RunState) RecordToolCall(audit ToolCallAudit) {
	for index := range s.ToolCallAudits {
		if s.ToolCallAudits[index].CallID == audit.CallID {
			s.Budget.ToolOutputBytes += audit.OutputBytes - s.ToolCallAudits[index].OutputBytes
			s.ToolCallAudits[index] = audit
			return
		}
	}
	s.ToolCallAudits = append(s.ToolCallAudits, audit)
	s.Budget.ToolCalls++
	s.Budget.ToolOutputBytes += audit.OutputBytes
}

func (s *RunState) RecordModelInvocation(invocation ModelInvocation) {
	s.ModelInvocations = append(s.ModelInvocations, invocation)
	s.Budget.ModelCalls++
	s.Budget.InputTokens += invocation.InputTokens
	s.Budget.OutputTokens += invocation.OutputTokens
	s.Budget.EstimatedCostUSD += invocation.EstimatedCostUSD
}

func (s *RunState) Clone() (*RunState, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("encode run state clone: %w", err)
	}
	var clone RunState
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, fmt.Errorf("decode run state clone: %w", err)
	}
	return &clone, nil
}

func (s *RunState) RequestCancellation(requestedBy, reason string) {
	if s.Cancellation.Requested() {
		return
	}
	now := time.Now().UTC()
	s.Cancellation = CancellationState{RequestedAt: &now, RequestedBy: requestedBy, Reason: reason}
	s.AppendEvent(EventCancellationAsked, s.CurrentNodeID, "Run cancellation requested")
}

func (s *RunState) RequestPause(requestedBy, reason string) {
	if s.Pause.Requested() || s.Status == StatusPaused {
		return
	}
	now := time.Now().UTC()
	s.Pause = PauseState{RequestedAt: &now, RequestedBy: requestedBy, Reason: reason, PreviousStatus: s.Status}
	s.AppendEvent(EventPauseRequested, s.CurrentNodeID, "Run pause requested")
}

func (s *RunState) AppendEvent(eventType EventType, nodeID, message string) {
	now := time.Now().UTC()
	s.UpdatedAt = now
	s.Events = append(s.Events, RunEvent{
		EventID: NewID(), TraceID: s.TraceID, RunID: s.RunID,
		Type: eventType, NodeID: nodeID, Message: message, CreatedAt: now,
	})
}

func (s *RunState) MarkNodeCompleted(nodeID string) {
	for _, completed := range s.CompletedNodeIDs {
		if completed == nodeID {
			return
		}
	}
	s.CompletedNodeIDs = append(s.CompletedNodeIDs, nodeID)
}

func NewID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("cannot obtain secure random bytes: " + err.Error())
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	buffer := make([]byte, 36)
	hex.Encode(buffer[0:8], value[0:4])
	buffer[8] = '-'
	hex.Encode(buffer[9:13], value[4:6])
	buffer[13] = '-'
	hex.Encode(buffer[14:18], value[6:8])
	buffer[18] = '-'
	hex.Encode(buffer[19:23], value[8:10])
	buffer[23] = '-'
	hex.Encode(buffer[24:36], value[10:16])
	return string(buffer)
}
