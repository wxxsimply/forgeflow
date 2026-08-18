package tool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/observability"
	"forgeflow/internal/policy"
)

const maxToolInputBytes = 256 * 1024

type Runtime struct {
	registry *Registry
	policy   policy.Engine
}

func NewRuntime(registry *Registry, policyEngine policy.Engine) (*Runtime, error) {
	if registry == nil || policyEngine == nil {
		return nil, apperror.New(apperror.CodeValidation, "tool registry and policy engine are required")
	}
	registry.Seal()
	return &Runtime{registry: registry, policy: policyEngine}, nil
}

func (r *Runtime) PolicyVersion() string { return r.policy.Version() }

func (r *Runtime) Execute(ctx context.Context, call CallContext, name string, input json.RawMessage) (invocation Invocation, resultErr error) {
	startedAt := time.Now().UTC()
	ctx, span := observability.StartToolSpan(ctx, call.RunID, call.TraceID, name, "", call.Agent, call.NodeID)
	defer func() {
		audit := invocation.Audit
		observability.DefaultMetrics().Tool(name, string(audit.Status), audit.PolicyAction, time.Since(startedAt))
		observability.EndSpan(span, resultErr, string(audit.Status))
	}()
	if call.CallID == "" {
		call.CallID = domain.NewID()
	}
	audit := domain.ToolCallAudit{
		CallID: call.CallID, NodeID: call.NodeID, Agent: call.Agent, ToolName: name,
		WorkspaceID: call.Workspace.ID, PolicyVersion: r.policy.Version(), CreatedAt: startedAt,
	}
	candidate, exists := r.registry.Get(name)
	if !exists {
		audit.Status, audit.PolicyAction, audit.ErrorCode = domain.ToolCallDenied, string(policy.ActionDeny), "unknown_tool"
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit}, apperror.New(apperror.CodePolicyDenied, "unknown tool is denied")
	}
	specification := normalizedSpec(candidate.Spec())
	audit.ToolVersion = specification.Version
	canonicalInput, inputHash, err := canonicalizeInput(input)
	if err != nil {
		audit.Status, audit.ErrorCode = domain.ToolCallFailed, string(apperror.CodeValidation)
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit}, err
	}
	audit.InputSHA256 = inputHash
	metadata, err := candidate.Analyze(canonicalInput)
	if err != nil {
		code := apperror.CodeOf(err)
		audit.Status, audit.ErrorCode = domain.ToolCallFailed, string(code)
		if code == apperror.CodePolicyDenied {
			audit.Status = domain.ToolCallDenied
		}
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit}, err
	}
	policyRequest := policy.Request{
		Phase: policy.PhaseBefore, RunID: call.RunID, Agent: call.Agent,
		ToolName: name, ToolVersion: specification.Version, Risk: specification.Risk,
		InputSHA256: inputHash, WorkspaceID: call.Workspace.ID, WorkspacePath: call.Workspace.Path,
		Metadata: metadata, Budget: call.Budget, Approval: call.Approval,
	}
	decision, err := r.policy.Evaluate(ctx, policyRequest)
	if err != nil {
		audit.Status, audit.ErrorCode = domain.ToolCallFailed, string(apperror.CodeInternal)
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit}, apperror.Wrap(err, apperror.CodeInternal, "tool.policy.before", "tool policy evaluation failed")
	}
	applyDecision(&audit, decision)
	if decision.Action == policy.ActionDeny {
		audit.Status, audit.ErrorCode = domain.ToolCallDenied, decision.Code
		completeAudit(&audit, startedAt)
		code := apperror.CodePolicyDenied
		if decision.Code == "budget_exhausted" {
			code = apperror.CodeBudget
		}
		return Invocation{Audit: audit}, apperror.New(code, decision.Reason)
	}
	if decision.Action == policy.ActionRequireApproval {
		audit.Status = domain.ToolCallApprovalRequired
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit, Approval: &ApprovalRequirement{
			CallID: call.CallID, ToolName: name, ToolVersion: specification.Version,
			InputSHA256: inputHash, WorkspaceID: call.Workspace.ID, PolicyVersion: decision.PolicyVersion,
			RuleID: decision.RuleID, Reason: decision.Reason, Scope: append([]string(nil), metadata.Paths...), Risk: decision.Risk,
		}}, nil
	}

	if call.OnStarted != nil {
		call.OnStarted()
	}
	executionContext, cancel := context.WithTimeout(ctx, specification.Timeout)
	output, executionErr := candidate.Execute(executionContext, call, canonicalInput)
	timedOut := executionContext.Err()
	cancel()
	if executionErr != nil || timedOut != nil {
		audit.Status = domain.ToolCallFailed
		code := apperror.CodeOf(executionErr)
		if timedOut != nil {
			code = apperror.CodeTimeout
			executionErr = timedOut
		}
		audit.ErrorCode = string(code)
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit}, apperror.Wrap(executionErr, code, "tool.execute", "tool execution failed")
	}
	if int64(len(output)) > specification.MaxOutputBytes {
		audit.Status, audit.ErrorCode, audit.OutputTruncated = domain.ToolCallFailed, string(apperror.CodeBudget), true
		audit.OutputBytes = specification.MaxOutputBytes
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit}, apperror.New(apperror.CodeBudget, "tool output exceeds the configured limit")
	}
	redacted, err := redactOutput(output)
	if err != nil {
		audit.Status, audit.ErrorCode = domain.ToolCallFailed, string(apperror.CodeInternal)
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit}, err
	}
	if int64(len(redacted)) > specification.MaxOutputBytes {
		audit.Status, audit.ErrorCode, audit.OutputTruncated = domain.ToolCallFailed, string(apperror.CodeBudget), true
		audit.OutputBytes = specification.MaxOutputBytes
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit}, apperror.New(apperror.CodeBudget, "redacted tool output exceeds the configured limit")
	}
	if err := candidate.ValidateOutput(redacted); err != nil {
		audit.Status, audit.ErrorCode = domain.ToolCallFailed, string(apperror.CodeInternal)
		completeAudit(&audit, startedAt)
		return Invocation{Audit: audit}, apperror.Wrap(err, apperror.CodeInternal, "tool.output.validate", "tool returned invalid output")
	}
	audit.OutputBytes = int64(len(redacted))
	policyRequest.Phase = policy.PhaseAfter
	policyRequest.OutputBytes = audit.OutputBytes
	afterDecision, err := r.policy.Evaluate(ctx, policyRequest)
	if err != nil || afterDecision.Action != policy.ActionAllow {
		audit.Status = domain.ToolCallDenied
		if err != nil {
			audit.ErrorCode = string(apperror.CodeInternal)
		} else {
			audit.ErrorCode = afterDecision.Code
			applyDecision(&audit, afterDecision)
		}
		completeAudit(&audit, startedAt)
		if err != nil {
			return Invocation{Audit: audit}, apperror.Wrap(err, apperror.CodeInternal, "tool.policy.after", "tool output policy evaluation failed")
		}
		code := apperror.CodePolicyDenied
		if afterDecision.Code == "budget_exhausted" {
			code = apperror.CodeBudget
		}
		return Invocation{Audit: audit}, apperror.New(code, afterDecision.Reason)
	}
	audit.Status = domain.ToolCallSucceeded
	applyDecision(&audit, afterDecision)
	completeAudit(&audit, startedAt)
	return Invocation{Output: redacted, Audit: audit}, nil
}

func normalizedSpec(specification Spec) Spec {
	if specification.Timeout <= 0 {
		specification.Timeout = 30 * time.Second
	}
	if specification.MaxOutputBytes <= 0 {
		specification.MaxOutputBytes = 256 * 1024
	}
	return specification
}

func canonicalizeInput(input json.RawMessage) (json.RawMessage, string, error) {
	if len(input) == 0 || len(input) > maxToolInputBytes {
		return nil, "", apperror.New(apperror.CodeValidation, "tool input is empty or exceeds the size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, "", apperror.Wrap(err, apperror.CodeValidation, "tool.input.decode", "tool input must be valid JSON")
	}
	if err := requireEOF(decoder); err != nil {
		return nil, "", apperror.Wrap(err, apperror.CodeValidation, "tool.input.decode", "tool input contains trailing data")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "", apperror.Wrap(err, apperror.CodeInternal, "tool.input.canonical", "tool input could not be canonicalized")
	}
	digest := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(digest[:]), nil
}

func requireEOF(decoder *json.Decoder) error {
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

func completeAudit(audit *domain.ToolCallAudit, startedAt time.Time) {
	now := time.Now().UTC()
	audit.CompletedAt = &now
	audit.LatencyMilliseconds = now.Sub(startedAt).Milliseconds()
}

func applyDecision(audit *domain.ToolCallAudit, decision policy.Decision) {
	audit.PolicyAction = string(decision.Action)
	audit.PolicyRuleID = decision.RuleID
	audit.PolicyVersion = decision.PolicyVersion
}
