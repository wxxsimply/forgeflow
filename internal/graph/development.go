package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/assessment"
	"forgeflow/internal/developer"
	"forgeflow/internal/domain"
	"forgeflow/internal/judge"
	"forgeflow/internal/planner"
	"forgeflow/internal/repository"
	"forgeflow/internal/reviewer"
	"forgeflow/internal/sandbox"
	"forgeflow/internal/security"
	toolruntime "forgeflow/internal/tool"
)

type DevelopmentOptions struct {
	Planner          planner.Planner
	Developer        developer.Developer
	Reviewer         reviewer.Reviewer
	Security         security.Security
	WorkspaceManager repository.WorkspaceManager
	ToolRuntime      *toolruntime.Runtime
	TestCommand      *domain.TestCommand
}

func DevelopmentDefinition(options DevelopmentOptions) (Definition, error) {
	if options.Planner == nil || options.Developer == nil || options.Reviewer == nil || options.Security == nil || options.WorkspaceManager == nil || options.ToolRuntime == nil {
		return Definition{}, apperror.New(apperror.CodeValidation, "development graph dependencies are required")
	}
	nodes := []Node{
		NodeFunc{NodeID: "start", Run: func(_ context.Context, state *domain.RunState) Result {
			state.Status = domain.StatusPlanning
			state.AppendEvent(domain.EventStatusChanged, "start", "Status changed to planning")
			return Result{Type: ResultCompleted, State: state}
		}},
		NodeFunc{NodeID: "planner", ExecutionPolicy: NodePolicy{Timeout: planner.Timeout(options.Planner), MaxAttempts: 1, ReplaySafe: false}, Run: func(ctx context.Context, state *domain.RunState) Result {
			planResult, err := options.Planner.CreatePlan(ctx, planner.Input{
				Task: state.Task, RepositoryPath: state.RepositoryPath, BaseRevision: state.BaseRevision, Budget: state.Budget,
			})
			if planResult.Invocation != nil {
				state.RecordModelInvocation(*planResult.Invocation)
				if allowed, reason := state.Budget.ModelUsageAllowed(); !allowed {
					return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeBudget, reason)}
				}
			}
			if err != nil {
				return Result{Type: ResultFatalError, State: state, Err: fmt.Errorf("create plan: %w", err)}
			}
			state.Plan = &planResult.Plan
			return Result{Type: ResultCompleted, State: state}
		}},
		NodeFunc{NodeID: "validate-plan", Run: func(_ context.Context, state *domain.RunState) Result {
			if state.Plan == nil {
				return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeValidation, "planner returned no plan")}
			}
			if err := state.Plan.Validate(); err != nil {
				return Result{Type: ResultFatalError, State: state, Err: apperror.Wrap(err, apperror.CodeValidation, "development.plan", "plan is invalid")}
			}
			state.Status = domain.StatusWaitingPlanApproval
			return Result{Type: ResultCompleted, State: state}
		}},
		NodeFunc{NodeID: "plan-approval", Run: approvalNode},
		NodeFunc{NodeID: "prepare-workspace", ExecutionPolicy: NodePolicy{Timeout: time.Minute, MaxAttempts: 1, ReplaySafe: false}, Run: func(ctx context.Context, state *domain.RunState) Result {
			state.Status = domain.StatusPreparingWorkspace
			workspace, err := options.WorkspaceManager.Prepare(ctx, domain.RepositoryRef{Path: state.RepositoryPath, BaseRevision: state.BaseRevision})
			if err != nil {
				return Result{Type: ResultFatalError, State: state, Err: err}
			}
			state.Workspace = &workspace
			command := options.TestCommand
			if command == nil {
				detected, err := DetectTestCommand(workspace.Path)
				if err != nil {
					return Result{Type: ResultFatalError, State: state, Err: err}
				}
				command = &detected
			}
			copyCommand := *command
			copyCommand.Args = append([]string(nil), command.Args...)
			copyCommand.EnvAllow = append([]string(nil), command.EnvAllow...)
			state.TestCommand = &copyCommand
			return Result{Type: ResultCompleted, State: state}
		}},
		developerNode(options.Developer),
		patchNode(options.ToolRuntime),
		NodeFunc{NodeID: "collect-diff", Run: func(ctx context.Context, state *domain.RunState) Result {
			if state.Workspace == nil || state.Plan == nil {
				return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeValidation, "diff requires workspace and approved plan")}
			}
			artifact, err := options.WorkspaceManager.Diff(ctx, *state.Workspace)
			if err != nil {
				return Result{Type: ResultFatalError, State: state, Err: err}
			}
			if err := validateDiffBudget(artifact, *state.Plan, state.Budget); err != nil {
				return Result{Type: ResultFatalError, State: state, Err: err}
			}
			state.Diff = &artifact
			state.ChangedFiles = append([]string(nil), artifact.ChangedFiles...)
			state.Status = domain.StatusEvaluating
			return Result{Type: ResultCompleted, State: state}
		}},
		testNode(options.ToolRuntime),
		assessmentParallelNode(options.Reviewer, options.Security),
		assessmentJoinNode(),
		NodeFunc{NodeID: "judge", Key: func(state *domain.RunState) string { return fmt.Sprintf("judge-%d", state.Iteration) }, Run: judgeNode},
		NodeFunc{NodeID: "end", Run: func(_ context.Context, state *domain.RunState) Result {
			state.Status = domain.StatusCompleted
			return Result{Type: ResultCompleted, State: state}
		}},
	}
	return Definition{
		EntryNodeID: "start", Nodes: nodes,
		Edges: []Edge{
			{From: "start", To: "planner"},
			{From: "planner", To: "validate-plan"},
			{From: "validate-plan", To: "plan-approval"},
			{From: "plan-approval", To: "prepare-workspace", When: func(state *domain.RunState) bool { return state.Status != domain.StatusCancelled }},
			{From: "prepare-workspace", To: "developer"},
			{From: "developer", To: "apply-patch"},
			{From: "apply-patch", To: "collect-diff", When: func(state *domain.RunState) bool { return state.Status != domain.StatusCancelled }},
			{From: "collect-diff", To: "run-test"},
			{From: "run-test", To: "parallel-assessments"},
			{From: "parallel-assessments", To: "assessment-join"},
			{From: "assessment-join", To: "judge"},
			{From: "judge", To: "end", When: func(state *domain.RunState) bool {
				return state.JudgeDecision != nil && (state.JudgeDecision.Action == domain.JudgePass || state.JudgeDecision.Action == domain.JudgePassWithApproval)
			}},
			{From: "judge", To: "developer", When: func(state *domain.RunState) bool { return state.Status == domain.StatusRepairing }},
		},
	}, nil
}

func developerNode(agent developer.Developer) Node {
	timeout := 5 * time.Minute
	if configured, ok := agent.(interface{ Timeout() time.Duration }); ok && configured.Timeout() > 0 {
		timeout = configured.Timeout()
	}
	return NodeFunc{
		NodeID: "developer", ExecutionPolicy: NodePolicy{Timeout: timeout, MaxAttempts: 1, ReplaySafe: false},
		Key: func(state *domain.RunState) string { return fmt.Sprintf("developer-%d", state.Iteration) },
		Run: func(ctx context.Context, state *domain.RunState) Result {
			if state.Plan == nil || state.Workspace == nil {
				return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeValidation, "developer requires an approved plan and workspace")}
			}
			state.Status = domain.StatusImplementing
			result, err := agent.Execute(ctx, developer.Input{
				RunID: state.RunID, NodeID: "developer", Task: state.Task, Plan: *state.Plan,
				Workspace: *state.Workspace, Budget: state.Budget,
				ToolNames:        []string{"list_files", "search_code", "read_file", "apply_patch", "run_test"},
				PreviousTest:     state.TestAssessment,
				CurrentDiff:      state.Diff,
				ReviewFindings:   repairReviewFindings(state.ReviewResult),
				SecurityFindings: repairSecurityFindings(state.SecurityResult),
			})
			if result.Invocation != nil {
				state.RecordModelInvocation(*result.Invocation)
				if allowed, reason := state.Budget.ModelUsageAllowed(); !allowed {
					return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeBudget, reason)}
				}
			}
			if err != nil {
				return Result{Type: ResultFatalError, State: state, Err: err}
			}
			if err := result.Implementation.Validate(); err != nil {
				return Result{Type: ResultFatalError, State: state, Err: apperror.Wrap(err, apperror.CodeModelOutput, "development.implementation", "developer result is invalid")}
			}
			if len(result.Implementation.UnresolvedIssues) > 0 {
				return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeApprovalNeeded, "developer reported unresolved issues requiring human review")}
			}
			state.Implementation = &result.Implementation
			state.JudgeDecision = nil
			return Result{Type: ResultCompleted, State: state}
		},
	}
}

func patchNode(runtime *toolruntime.Runtime) Node {
	return ToolNode{
		NodeID: "apply-patch", Runtime: runtime, ToolName: "apply_patch", Agent: "developer",
		Input: func(state *domain.RunState) json.RawMessage {
			if state.Implementation == nil {
				return json.RawMessage(`{}`)
			}
			return mustJSON(toolruntime.ApplyPatchInput{Patch: state.Implementation.Patch, ExpectedFiles: state.Implementation.ChangedFiles})
		},
		AuthorizedPaths: func(state *domain.RunState) []string {
			if state.Plan == nil {
				return nil
			}
			return state.Plan.FilesLikelyAffected
		},
		OnOutput: func(state *domain.RunState, output json.RawMessage, _ domain.ToolCallAudit) error {
			var applied toolruntime.ApplyPatchOutput
			if err := json.Unmarshal(output, &applied); err != nil {
				return apperror.Wrap(err, apperror.CodeInternal, "development.patch.output", "patch tool output is invalid")
			}
			if state.Implementation == nil || !state.Implementation.FilesMatch(applied.AppliedFiles) {
				return apperror.New(apperror.CodePolicyDenied, "developer changedFiles does not match the applied patch")
			}
			state.Status = domain.StatusImplementing
			return nil
		},
	}
}

func testNode(runtime *toolruntime.Runtime) Node {
	return ToolNode{
		NodeID: "run-test", Runtime: runtime, ToolName: "run_test", Agent: "tester",
		Input: func(state *domain.RunState) json.RawMessage {
			if state.TestCommand == nil {
				return json.RawMessage(`{}`)
			}
			return mustJSON(toolruntime.CommandInput{
				Program: state.TestCommand.Program, Args: state.TestCommand.Args,
				WorkingDir: state.TestCommand.WorkingDir, EnvAllow: state.TestCommand.EnvAllow,
				TimeoutMilliseconds: state.TestCommand.Timeout.Milliseconds(),
			})
		},
		OnOutput: func(state *domain.RunState, output json.RawMessage, audit domain.ToolCallAudit) error {
			var result sandbox.Result
			if err := json.Unmarshal(output, &result); err != nil {
				return apperror.Wrap(err, apperror.CodeInternal, "development.test.output", "test tool output is invalid")
			}
			if state.TestCommand == nil {
				return apperror.New(apperror.CodeInternal, "test command disappeared from run state")
			}
			state.TestAssessment = &domain.TestAssessment{
				ToolCallID: audit.CallID, Program: state.TestCommand.Program,
				Args: append([]string(nil), state.TestCommand.Args...), ExitCode: result.ExitCode,
				Stdout: result.Stdout, Stderr: result.Stderr, Duration: result.Duration,
				Truncated: result.Truncated, Passed: result.ExitCode == 0, CompletedAt: time.Now().UTC(),
			}
			state.Status = domain.StatusEvaluating
			return nil
		},
	}
}

type reviewerBranchResult struct {
	Review     domain.ReviewResult     `json:"review"`
	Invocation *domain.ModelInvocation `json:"invocation,omitempty"`
}

type securityBranchResult struct {
	Security   domain.SecurityResult   `json:"security"`
	Invocation *domain.ModelInvocation `json:"invocation,omitempty"`
}

func assessmentParallelNode(reviewAgent reviewer.Reviewer, securityAgent security.Security) Node {
	timeout := 2 * time.Minute
	for _, agent := range []any{reviewAgent, securityAgent} {
		if configured, ok := agent.(interface{ Timeout() time.Duration }); ok && configured.Timeout() > timeout {
			timeout = configured.Timeout()
		}
	}
	return ParallelNode{
		NodeID: "parallel-assessments", MaxConcurrency: 2,
		ExecutionPolicy: NodePolicy{Timeout: timeout, MaxAttempts: 1, ReplaySafe: false},
		Branches: []Branch{
			BranchFunc{BranchID: "reviewer", Run: func(ctx context.Context, snapshot *domain.RunState) (json.RawMessage, error) {
				input, err := assessmentInput(snapshot, "reviewer")
				if err != nil {
					return nil, err
				}
				result, executeErr := reviewAgent.Execute(ctx, input)
				payload, marshalErr := json.Marshal(reviewerBranchResult{Review: result.Review, Invocation: result.Invocation})
				if marshalErr != nil {
					return nil, marshalErr
				}
				return payload, executeErr
			}},
			BranchFunc{BranchID: "security", Run: func(ctx context.Context, snapshot *domain.RunState) (json.RawMessage, error) {
				input, err := assessmentInput(snapshot, "security")
				if err != nil {
					return nil, err
				}
				result, executeErr := securityAgent.Execute(ctx, input)
				payload, marshalErr := json.Marshal(securityBranchResult{Security: result.Security, Invocation: result.Invocation})
				if marshalErr != nil {
					return nil, marshalErr
				}
				return payload, executeErr
			}},
		},
	}
}

func assessmentInput(state *domain.RunState, nodeID string) (assessment.Input, error) {
	if state.Plan == nil || state.Workspace == nil || state.Diff == nil || state.TestAssessment == nil {
		return assessment.Input{}, apperror.New(apperror.CodeValidation, "assessment requires plan, workspace, diff, and test evidence")
	}
	return assessment.Input{
		RunID: state.RunID, NodeID: nodeID, Task: state.Task, Plan: *state.Plan,
		Workspace: *state.Workspace, Diff: *state.Diff, Test: *state.TestAssessment, Budget: state.Budget,
	}, nil
}

func assessmentJoinNode() Node {
	return JoinNode{
		NodeID: "assessment-join", SourceNodeID: "parallel-assessments", BranchIDs: []string{"reviewer", "security"},
		Decide: func(state *domain.RunState, branches map[string]domain.BranchState) Result {
			state.AssessmentErrors = map[string]string{}
			state.ReviewResult = nil
			state.SecurityResult = nil
			for _, branchID := range []string{"reviewer", "security"} {
				branch := branches[branchID]
				if branchID == "reviewer" {
					var decoded reviewerBranchResult
					if len(branch.Result) > 0 {
						if err := json.Unmarshal(branch.Result, &decoded); err != nil {
							state.AssessmentErrors[branchID] = "invalid reviewer branch result"
						} else if decoded.Invocation != nil {
							state.RecordModelInvocation(*decoded.Invocation)
						}
					}
					if branch.Status == domain.BranchSucceeded {
						if err := decoded.Review.Validate(); err != nil {
							state.AssessmentErrors[branchID] = err.Error()
						} else {
							state.ReviewResult = &decoded.Review
						}
					}
				} else {
					var decoded securityBranchResult
					if len(branch.Result) > 0 {
						if err := json.Unmarshal(branch.Result, &decoded); err != nil {
							state.AssessmentErrors[branchID] = "invalid security branch result"
						} else if decoded.Invocation != nil {
							state.RecordModelInvocation(*decoded.Invocation)
						}
					}
					if branch.Status == domain.BranchSucceeded {
						if state.Diff != nil {
							decoded.Security.Findings = security.MergeFindings(security.DeterministicFindings(*state.Diff), decoded.Security.Findings)
						}
						if err := decoded.Security.Validate(); err != nil {
							state.AssessmentErrors[branchID] = err.Error()
						} else {
							state.SecurityResult = &decoded.Security
						}
					}
				}
				if branch.Status != domain.BranchSucceeded {
					message := branch.Error
					if strings.TrimSpace(message) == "" {
						message = "assessment branch did not succeed"
					}
					state.AssessmentErrors[branchID] = message
				}
			}
			if allowed, reason := state.Budget.ModelUsageAllowed(); !allowed {
				state.AssessmentErrors["budget"] = reason
			}
			state.Status = domain.StatusEvaluating
			return Result{Type: ResultCompleted, State: state}
		},
	}
}

func judgeNode(_ context.Context, state *domain.RunState) Result {
	workspaceID := ""
	if state.Workspace != nil {
		workspaceID = state.Workspace.ID
	}
	decision := judge.Evaluate(judge.Input{
		Test: state.TestAssessment, Review: state.ReviewResult, Security: state.SecurityResult,
		AssessmentErrors: state.AssessmentErrors, Diff: state.Diff, Plan: state.Plan,
		Budget: state.Budget, RepairCount: state.RepairCount, Iteration: state.Iteration, WorkspaceID: workspaceID,
	})
	state.JudgeDecision = &decision

	if decision.Action == domain.JudgeHumanReview {
		if pending := state.PendingApproval; pending != nil {
			if pending.ActionType != "judge_security" {
				return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeConflict, "pending approval belongs to another action")}
			}
			if pending.InputSHA256 != decision.InputSHA256 || pending.WorkspaceID != workspaceID || pending.PolicyVersion != judge.Version {
				return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodePolicyDenied, "judge approval evidence does not match current inputs")}
			}
			switch pending.Status {
			case domain.ApprovalPending:
				return Result{Type: ResultInterrupted, State: state, Approval: pending}
			case domain.ApprovalRejected:
				state.JudgeDecisions = append(state.JudgeDecisions, decision)
				state.PendingApproval = nil
				state.Status = domain.StatusCancelled
				return Result{Type: ResultCompleted, State: state}
			case domain.ApprovalApproved:
				decision.Action = domain.JudgePassWithApproval
				decision.Reasons = append(decision.Reasons, "human security review approved the exact evidence digest")
				state.JudgeDecision = &decision
				state.JudgeDecisions = append(state.JudgeDecisions, decision)
				state.PendingApproval = nil
				return Result{Type: ResultCompleted, State: state}
			default:
				return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeConflict, "judge approval status is invalid")}
			}
		}
		state.JudgeDecisions = append(state.JudgeDecisions, decision)
		approval := &domain.ApprovalRequest{
			ApprovalID: domain.NewID(), RunID: state.RunID, ActionType: "judge_security",
			Reason: strings.Join(decision.Reasons, "; "), Scope: append([]string(nil), decision.FindingIDs...),
			Risk: domain.RiskHigh, Status: domain.ApprovalPending, RequestedAt: time.Now().UTC(),
			InputSHA256: decision.InputSHA256, WorkspaceID: workspaceID, PolicyVersion: judge.Version,
		}
		state.Status = domain.StatusWaitingActionApproval
		state.AppendEvent(domain.EventApprovalRequested, "judge", "High-risk security review approval requested")
		return Result{Type: ResultInterrupted, State: state, Approval: approval}
	}

	state.JudgeDecisions = append(state.JudgeDecisions, decision)
	switch decision.Action {
	case domain.JudgePass, domain.JudgePassWithApproval:
		return Result{Type: ResultCompleted, State: state}
	case domain.JudgeRepair:
		state.RepairCount++
		state.Iteration++
		state.Status = domain.StatusRepairing
		state.Implementation = nil
		return Result{Type: ResultCompleted, State: state}
	case domain.JudgeFail:
		return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeValidation, strings.Join(decision.Reasons, "; "))}
	default:
		return Result{Type: ResultFatalError, State: state, Err: apperror.New(apperror.CodeInternal, "judge returned an unknown action")}
	}
}

func repairReviewFindings(result *domain.ReviewResult) []domain.ReviewFinding {
	if result == nil {
		return nil
	}
	findings := make([]domain.ReviewFinding, 0)
	for _, finding := range result.Findings {
		if finding.Severity == domain.SeverityBlocking {
			findings = append(findings, finding)
		}
	}
	return findings
}

func repairSecurityFindings(result *domain.SecurityResult) []domain.SecurityFinding {
	if result == nil {
		return nil
	}
	findings := make([]domain.SecurityFinding, 0)
	for _, finding := range result.Findings {
		if finding.Severity == domain.SeverityHigh || finding.Severity == domain.SeverityCritical {
			findings = append(findings, finding)
		}
	}
	return findings
}

func DetectTestCommand(workspace string) (domain.TestCommand, error) {
	candidates := []struct {
		file    string
		command domain.TestCommand
	}{
		{file: "go.mod", command: domain.TestCommand{Program: "go", Args: []string{"test", "./..."}}},
		{file: "package.json", command: domain.TestCommand{Program: "npm", Args: []string{"test"}}},
		{file: "Cargo.toml", command: domain.TestCommand{Program: "cargo", Args: []string{"test"}}},
		{file: "pyproject.toml", command: domain.TestCommand{Program: "python", Args: []string{"-m", "pytest"}}},
		{file: "pytest.ini", command: domain.TestCommand{Program: "python", Args: []string{"-m", "pytest"}}},
	}
	for _, candidate := range candidates {
		info, err := os.Stat(filepath.Join(workspace, candidate.file))
		if err == nil && info.Mode().IsRegular() {
			candidate.command.WorkingDir = "."
			candidate.command.EnvAllow = []string{"CI"}
			candidate.command.Timeout = 10 * time.Minute
			return candidate.command, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return domain.TestCommand{}, apperror.Wrap(err, apperror.CodeForbidden, "development.test.detect", "build configuration is inaccessible")
		}
	}
	return domain.TestCommand{}, apperror.New(apperror.CodeValidation, "no allowlisted test command could be detected")
}

func validateDiffBudget(artifact domain.DiffArtifact, plan domain.ExecutionPlan, budget domain.RunBudget) error {
	if len(artifact.ChangedFiles) == 0 {
		return apperror.New(apperror.CodeValidation, "implementation produced an empty diff")
	}
	if budget.MaxChangedFiles > 0 && len(artifact.ChangedFiles) > budget.MaxChangedFiles {
		return apperror.New(apperror.CodeBudget, "changed file budget exceeded")
	}
	if budget.MaxDiffBytes > 0 && artifact.Size > budget.MaxDiffBytes {
		return apperror.New(apperror.CodeBudget, "diff byte budget exceeded")
	}
	changedLines := 0
	for _, line := range strings.Split(artifact.Patch, "\n") {
		if (strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")) || (strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")) {
			changedLines++
		}
	}
	if budget.MaxDiffLines > 0 && changedLines > budget.MaxDiffLines {
		return apperror.New(apperror.CodeBudget, "diff line budget exceeded")
	}
	approved := append([]string(nil), plan.FilesLikelyAffected...)
	slices.Sort(approved)
	for _, file := range artifact.ChangedFiles {
		if _, exists := slices.BinarySearch(approved, file); !exists {
			return apperror.New(apperror.CodePolicyDenied, "actual diff contains a file outside the approved plan")
		}
	}
	return nil
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}
