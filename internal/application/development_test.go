package application

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgeflow/internal/checkpoint"
	"forgeflow/internal/developer"
	"forgeflow/internal/domain"
	"forgeflow/internal/graph"
	"forgeflow/internal/planner"
	"forgeflow/internal/policy"
	"forgeflow/internal/repository"
	"forgeflow/internal/reviewer"
	"forgeflow/internal/sandbox"
	"forgeflow/internal/security"
	"forgeflow/internal/tool"
)

const developmentTestImage = "example/forgeflow@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

type fixturePlanner struct{}

func (fixturePlanner) CreatePlan(context.Context, planner.Input) (planner.Result, error) {
	return planner.Result{Plan: domain.ExecutionPlan{
		Summary: "fix Add", Assumptions: []string{}, FilesLikelyAffected: []string{"calc.go"},
		Steps:        []domain.PlanStep{{ID: "fix", Description: "correct addition", AcceptanceCriteria: []string{"Add returns a+b"}, DependsOn: []string{}}},
		Risks:        []domain.PlanRisk{{Level: domain.RiskLow, Description: "small arithmetic fix"}},
		TestStrategy: []string{"go test ./..."},
	}}, nil
}

type actualFixtureRunner struct{ requests []sandbox.Request }

func (r *actualFixtureRunner) Run(ctx context.Context, request sandbox.Request) (sandbox.Result, error) {
	r.requests = append(r.requests, request)
	command := exec.CommandContext(ctx, request.Program, request.Args...)
	command.Dir = filepath.Join(request.WorkspacePath, filepath.FromSlash(request.WorkingDir))
	command.Env = os.Environ()
	for name, value := range request.Environment {
		command.Env = append(command.Env, name+"="+value)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	startedAt := time.Now()
	err := command.Run()
	result := sandbox.Result{ExitCode: 0, Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(startedAt)}
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			result.ExitCode = exitError.ExitCode()
			return result, nil
		}
		return result, err
	}
	return result, nil
}

func TestDevelopmentFlowAppliesApprovedPatchRunsRealTestAndPreservesOriginal(t *testing.T) {
	repositoryPath := createDevelopmentFixture(t)
	original, err := os.ReadFile(filepath.Join(repositoryPath, "calc.go"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "workspaces")
	manager, err := repository.NewGitWorkspaceManager(workspaceRoot, repository.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	implementationAgent := &developer.Fake{Results: []developer.Result{{Implementation: domain.ImplementationResult{
		Summary: "correct Add", Patch: calcPatch("a - b", "a + b"), ChangedFiles: []string{"calc.go"},
		Evidence: []string{"inspected calc.go"}, UnresolvedIssues: []string{}, RequestedApprovals: []string{},
	}}}}
	runner := &actualFixtureRunner{}
	definition := developmentDefinition(t, manager, implementationAgent, runner)
	storeDirectory := t.TempDir()

	service := NewServiceWithDefinition(checkpoint.NewFileStore(storeDirectory), definition)
	waitingPlan, err := service.Create(context.Background(), CreateInput{Task: "fix Add", RepositoryPath: repositoryPath, BaseRevision: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if waitingPlan.Status != domain.StatusWaitingPlanApproval || waitingPlan.PendingApproval == nil {
		t.Fatalf("plan state=%s approval=%+v", waitingPlan.Status, waitingPlan.PendingApproval)
	}

	service = NewServiceWithDefinition(checkpoint.NewFileStore(storeDirectory), definition)
	waitingPatch, err := service.ResolveApproval(context.Background(), waitingPlan.RunID, true, "plan approved")
	if err != nil {
		t.Fatal(err)
	}
	if waitingPatch.Status != domain.StatusWaitingActionApproval || waitingPatch.PendingApproval == nil || waitingPatch.PendingApproval.ToolName != "apply_patch" {
		t.Fatalf("patch state=%s approval=%+v", waitingPatch.Status, waitingPatch.PendingApproval)
	}
	if content, _ := os.ReadFile(filepath.Join(repositoryPath, "calc.go")); string(content) != string(original) {
		t.Fatal("original repository changed before patch approval")
	}

	service = NewServiceWithDefinition(checkpoint.NewFileStore(storeDirectory), definition)
	completed, err := service.ResolveApproval(context.Background(), waitingPatch.RunID, true, "patch approved")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.StatusCompleted || completed.TestAssessment == nil || !completed.TestAssessment.Passed || completed.TestAssessment.ExitCode != 0 {
		t.Fatalf("completed status=%s test=%+v error=%+v", completed.Status, completed.TestAssessment, completed.Error)
	}
	if completed.Diff == nil || completed.Diff.SHA256 == "" || !strings.Contains(completed.Diff.Patch, "a + b") {
		t.Fatalf("diff=%+v", completed.Diff)
	}
	if len(runner.requests) != 1 || runner.requests[0].Program != "go" || strings.Join(runner.requests[0].Args, " ") != "test ./..." {
		t.Fatalf("actual test requests=%+v", runner.requests)
	}
	workspaceContent, err := os.ReadFile(filepath.Join(completed.Workspace.Path, "calc.go"))
	if err != nil || !strings.Contains(string(workspaceContent), "a + b") {
		t.Fatalf("workspace content=%q err=%v", workspaceContent, err)
	}
	after, err := os.ReadFile(filepath.Join(repositoryPath, "calc.go"))
	if err != nil || string(after) != string(original) {
		t.Fatalf("original repository was modified: content=%q err=%v", after, err)
	}
	if status := gitOutput(t, repositoryPath, "status", "--porcelain=v1"); status != "" {
		t.Fatalf("original repository is dirty: %s", status)
	}
	if err := manager.Cleanup(context.Background(), *completed.Workspace); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentFlowAllowsOnlyOneRepair(t *testing.T) {
	repositoryPath := createDevelopmentFixture(t)
	manager, err := repository.NewGitWorkspaceManager(filepath.Join(t.TempDir(), "workspaces"), repository.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	agent := &developer.Fake{Results: []developer.Result{
		{Implementation: domain.ImplementationResult{Summary: "first fix", Patch: calcPatch("a - b", "a + b"), ChangedFiles: []string{"calc.go"}, Evidence: []string{}, UnresolvedIssues: []string{}, RequestedApprovals: []string{}}},
		{Implementation: domain.ImplementationResult{Summary: "repair", Patch: calcPatch("a + b", "a + b + 0"), ChangedFiles: []string{"calc.go"}, Evidence: []string{"addressed recorded failure"}, UnresolvedIssues: []string{}, RequestedApprovals: []string{}}},
	}}
	runner := &sandbox.FakeRunner{Results: []sandbox.Result{
		{ExitCode: 1, Stderr: "recorded failure"}, {ExitCode: 0, Stdout: "ok"},
	}}
	definition := developmentDefinition(t, manager, agent, runner)
	service := NewServiceWithDefinition(checkpoint.NewFileStore(t.TempDir()), definition)
	waiting, err := service.Create(context.Background(), CreateInput{Task: "fix Add", RepositoryPath: repositoryPath})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err = service.ResolveApproval(context.Background(), waiting.RunID, true, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != domain.StatusWaitingActionApproval || waiting.PendingApproval == nil {
		t.Fatalf("first patch approval was not requested: status=%s error=%+v approval=%+v", waiting.Status, waiting.Error, waiting.PendingApproval)
	}
	waiting, err = service.ResolveApproval(context.Background(), waiting.RunID, true, "first patch")
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != domain.StatusWaitingActionApproval || waiting.RepairCount != 1 || agent.CallCount() != 2 {
		t.Fatalf("repair waiting status=%s repairs=%d developerCalls=%d", waiting.Status, waiting.RepairCount, agent.CallCount())
	}
	completed, err := service.ResolveApproval(context.Background(), waiting.RunID, true, "repair patch")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.StatusCompleted || completed.RepairCount != 1 || runner.CallCount() != 2 || !completed.TestAssessment.Passed {
		t.Fatalf("completed status=%s repairs=%d test=%+v calls=%d", completed.Status, completed.RepairCount, completed.TestAssessment, runner.CallCount())
	}
	if len(agent.Inputs) != 2 || agent.Inputs[1].PreviousTest == nil || agent.Inputs[1].PreviousTest.ExitCode != 1 || agent.Inputs[1].CurrentDiff == nil {
		t.Fatalf("repair input did not contain bounded failure evidence: %+v", agent.Inputs)
	}
	if err := manager.Cleanup(context.Background(), *completed.Workspace); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentFlowRejectsPatchApprovalWithoutTouchingOriginal(t *testing.T) {
	repositoryPath := createDevelopmentFixture(t)
	original, _ := os.ReadFile(filepath.Join(repositoryPath, "calc.go"))
	manager, err := repository.NewGitWorkspaceManager(filepath.Join(t.TempDir(), "workspaces"), repository.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	agent := &developer.Fake{Results: []developer.Result{{Implementation: domain.ImplementationResult{
		Summary: "fix", Patch: calcPatch("a - b", "a + b"), ChangedFiles: []string{"calc.go"},
		Evidence: []string{}, UnresolvedIssues: []string{}, RequestedApprovals: []string{},
	}}}}
	definition := developmentDefinition(t, manager, agent, &sandbox.FakeRunner{})
	service := NewServiceWithDefinition(checkpoint.NewFileStore(t.TempDir()), definition)
	waiting, err := service.Create(context.Background(), CreateInput{Task: "fix", RepositoryPath: repositoryPath})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err = service.ResolveApproval(context.Background(), waiting.RunID, true, "plan")
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.ResolveApproval(context.Background(), waiting.RunID, false, "reject patch")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("status=%s", cancelled.Status)
	}
	after, _ := os.ReadFile(filepath.Join(repositoryPath, "calc.go"))
	if string(after) != string(original) {
		t.Fatal("rejected patch modified original repository")
	}
	if err := manager.Cleanup(context.Background(), *cancelled.Workspace); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentFlowDeterministicSecurityCannotBeOverridden(t *testing.T) {
	repositoryPath := createDevelopmentFixture(t)
	manager, err := repository.NewGitWorkspaceManager(filepath.Join(t.TempDir(), "workspaces"), repository.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	implementationAgent := &developer.Fake{Results: []developer.Result{{Implementation: domain.ImplementationResult{
		Summary: "fix with leaked credential", Patch: credentialPatch(), ChangedFiles: []string{"calc.go"},
		Evidence: []string{}, UnresolvedIssues: []string{}, RequestedApprovals: []string{},
	}}}}
	definition := developmentDefinitionWithAssessors(t, manager, implementationAgent, &sandbox.FakeRunner{Results: []sandbox.Result{{ExitCode: 0}}}, passingReviewer(), passingSecurity())
	budget := domain.DefaultRunBudget(2)
	budget.MaxRepairs = 0
	failed := executeThroughPatchApproval(t, definition, repositoryPath, &budget)
	if failed.Status != domain.StatusFailed || failed.JudgeDecision == nil || failed.JudgeDecision.Action != domain.JudgeFail {
		t.Fatalf("status=%s decision=%+v error=%+v", failed.Status, failed.JudgeDecision, failed.Error)
	}
	if failed.SecurityResult == nil || len(failed.SecurityResult.Findings) == 0 || !strings.HasPrefix(failed.SecurityResult.Findings[0].ID, "SEC-DET-") {
		t.Fatalf("deterministic security finding was lost: %+v", failed.SecurityResult)
	}
}

func TestDevelopmentFlowBlockingReviewAndBranchFailureAreExplicit(t *testing.T) {
	for name, reviewAgent := range map[string]reviewer.Reviewer{
		"blocking finding": &reviewer.Fake{Results: []reviewer.Result{{Review: domain.ReviewResult{Summary: "blocking", Findings: []domain.ReviewFinding{{
			ID: "REV-BLOCK", Severity: domain.SeverityBlocking, File: "calc.go", Title: "incorrect behavior",
			Evidence: "the diff violates the acceptance criterion", FailureMode: "the requested operation returns a wrong value",
			Recommendation: "correct the implementation", Confirmed: true,
		}}}}}},
		"branch timeout": &reviewer.Fake{Errors: []error{context.DeadlineExceeded}},
	} {
		t.Run(name, func(t *testing.T) {
			repositoryPath := createDevelopmentFixture(t)
			manager, err := repository.NewGitWorkspaceManager(filepath.Join(t.TempDir(), "workspaces"), repository.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			implementationAgent := &developer.Fake{Results: []developer.Result{{Implementation: domain.ImplementationResult{
				Summary: "fix", Patch: calcPatch("a - b", "a + b"), ChangedFiles: []string{"calc.go"},
				Evidence: []string{}, UnresolvedIssues: []string{}, RequestedApprovals: []string{},
			}}}}
			definition := developmentDefinitionWithAssessors(t, manager, implementationAgent, &sandbox.FakeRunner{Results: []sandbox.Result{{ExitCode: 0}}}, reviewAgent, passingSecurity())
			budget := domain.DefaultRunBudget(2)
			budget.MaxRepairs = 0
			failed := executeThroughPatchApproval(t, definition, repositoryPath, &budget)
			if failed.Status != domain.StatusFailed || failed.JudgeDecision == nil || failed.JudgeDecision.Action != domain.JudgeFail {
				t.Fatalf("status=%s decision=%+v error=%+v", failed.Status, failed.JudgeDecision, failed.Error)
			}
			if name == "branch timeout" && !strings.Contains(failed.AssessmentErrors["reviewer"], "deadline") {
				t.Fatalf("branch failure was not explicit: %+v", failed.AssessmentErrors)
			}
		})
	}
}

func TestDevelopmentFlowUncertainHighSecurityRequiresBoundApproval(t *testing.T) {
	repositoryPath := createDevelopmentFixture(t)
	manager, err := repository.NewGitWorkspaceManager(filepath.Join(t.TempDir(), "workspaces"), repository.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	implementationAgent := &developer.Fake{Results: []developer.Result{{Implementation: domain.ImplementationResult{
		Summary: "fix", Patch: calcPatch("a - b", "a + b"), ChangedFiles: []string{"calc.go"},
		Evidence: []string{}, UnresolvedIssues: []string{}, RequestedApprovals: []string{},
	}}}}
	securityAgent := &security.Fake{Results: []security.Result{{Security: domain.SecurityResult{Summary: "needs human review", Findings: []domain.SecurityFinding{{
		ID: "SEC-HUMAN", Severity: domain.SeverityHigh, File: "calc.go", Title: "uncertain trust boundary",
		Evidence: "the caller contract is not visible", Impact: "authorization could be bypassed",
		Recommendation: "verify the caller contract", Confirmed: false, HumanReview: true,
	}}}}}}
	definition := developmentDefinitionWithAssessors(t, manager, implementationAgent, &sandbox.FakeRunner{Results: []sandbox.Result{{ExitCode: 0}}}, passingReviewer(), securityAgent)
	store := checkpoint.NewFileStore(t.TempDir())
	service := NewServiceWithDefinition(store, definition)
	waiting, err := service.Create(context.Background(), CreateInput{Task: "fix", RepositoryPath: repositoryPath})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err = service.ResolveApproval(context.Background(), waiting.RunID, true, "plan")
	if err != nil {
		t.Fatal(err)
	}
	waiting, err = service.ResolveApproval(context.Background(), waiting.RunID, true, "patch")
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != domain.StatusWaitingActionApproval || waiting.PendingApproval == nil || waiting.PendingApproval.ActionType != "judge_security" || waiting.PendingApproval.InputSHA256 == "" {
		t.Fatalf("judge approval=%+v status=%s", waiting.PendingApproval, waiting.Status)
	}
	completed, err := service.ResolveApproval(context.Background(), waiting.RunID, true, "security reviewed")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.StatusCompleted || completed.JudgeDecision == nil || completed.JudgeDecision.Action != domain.JudgePassWithApproval {
		t.Fatalf("status=%s decision=%+v error=%+v", completed.Status, completed.JudgeDecision, completed.Error)
	}
}

func developmentDefinition(t *testing.T, manager repository.WorkspaceManager, agent developer.Developer, runner sandbox.Runner) graph.Definition {
	return developmentDefinitionWithAssessors(t, manager, agent, runner, passingReviewer(), passingSecurity())
}

func developmentDefinitionWithAssessors(t *testing.T, manager repository.WorkspaceManager, agent developer.Developer, runner sandbox.Runner, reviewAgent reviewer.Reviewer, securityAgent security.Security) graph.Definition {
	t.Helper()
	registry := tool.NewRegistry()
	if err := tool.RegisterMutationTools(registry, tool.DefaultPatchLimits()); err != nil {
		t.Fatal(err)
	}
	if err := tool.RegisterCommandTools(registry, runner, developmentTestImage); err != nil {
		t.Fatal(err)
	}
	runtime, err := tool.NewRuntime(registry, policy.DefaultEngine())
	if err != nil {
		t.Fatal(err)
	}
	definition, err := graph.DevelopmentDefinition(graph.DevelopmentOptions{
		Planner: fixturePlanner{}, Developer: agent, Reviewer: reviewAgent, Security: securityAgent,
		WorkspaceManager: manager, ToolRuntime: runtime,
	})
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func executeThroughPatchApproval(t *testing.T, definition graph.Definition, repositoryPath string, budget *domain.RunBudget) *domain.RunState {
	t.Helper()
	service := NewServiceWithDefinition(checkpoint.NewFileStore(t.TempDir()), definition)
	waiting, err := service.Create(context.Background(), CreateInput{Task: "fix", RepositoryPath: repositoryPath, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err = service.ResolveApproval(context.Background(), waiting.RunID, true, "plan")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ResolveApproval(context.Background(), waiting.RunID, true, "patch")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func passingReviewer() reviewer.Reviewer {
	results := make([]reviewer.Result, 4)
	for index := range results {
		results[index].Review = domain.ReviewResult{Summary: "no blocking review findings", Findings: []domain.ReviewFinding{}}
	}
	return &reviewer.Fake{Results: results}
}

func passingSecurity() security.Security {
	results := make([]security.Result, 4)
	for index := range results {
		results[index].Security = domain.SecurityResult{Summary: "no high security findings", Findings: []domain.SecurityFinding{}}
	}
	return &security.Fake{Results: results}
}

func createDevelopmentFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeDevelopmentFile(t, root, "go.mod", "module fixture\n\ngo 1.22\n")
	writeDevelopmentFile(t, root, "calc.go", "package fixture\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n")
	writeDevelopmentFile(t, root, "calc_test.go", "package fixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 { t.Fatal(\"bad sum\") }\n}\n")
	gitOutput(t, root, "init")
	gitOutput(t, root, "config", "user.email", "forgeflow@example.invalid")
	gitOutput(t, root, "config", "user.name", "ForgeFlow Test")
	gitOutput(t, root, "add", ".")
	gitOutput(t, root, "commit", "-m", "fixture")
	return root
}

func calcPatch(before, after string) string {
	return "diff --git a/calc.go b/calc.go\n" +
		"--- a/calc.go\n" +
		"+++ b/calc.go\n" +
		"@@ -1,5 +1,5 @@\n" +
		" package fixture\n" +
		" \n" +
		" func Add(a, b int) int {\n" +
		"-\treturn " + before + "\n" +
		"+\treturn " + after + "\n" +
		" }\n"
}

func credentialPatch() string {
	return "diff --git a/calc.go b/calc.go\n" +
		"--- a/calc.go\n" +
		"+++ b/calc.go\n" +
		"@@ -1,5 +1,6 @@\n" +
		" package fixture\n" +
		" \n" +
		" func Add(a, b int) int {\n" +
		"+\t// api_key = \"supersecretvalue\"\n" +
		"-\treturn a - b\n" +
		"+\treturn a + b\n" +
		" }\n"
}

func writeDevelopmentFile(t *testing.T, root, relative, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, relative), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", directory}, args...)
	command := exec.Command("git", commandArgs...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

var _ sandbox.Runner = (*actualFixtureRunner)(nil)
