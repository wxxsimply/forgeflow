package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"forgeflow/internal/domain"
)

type Validator interface {
	Capture(*domain.RunState) domain.ResumeGuard
	Validate(context.Context, *domain.RunState) error
}

type Options struct {
	GitBinary              string
	ExpectedPolicyVersions []string
	ExpectedPromptVersions map[string]string
}

type ResumeValidator struct {
	gitBinary              string
	expectedPolicyVersions []string
	expectedPromptVersions map[string]string
}

func NewValidator(options Options) (*ResumeValidator, error) {
	if options.GitBinary == "" {
		options.GitBinary = "git"
	}
	if options.GitBinary != "git" && strings.ContainsAny(options.GitBinary, `/\`) {
		return nil, fmt.Errorf("resume validator git binary must be a bare executable")
	}
	policyVersions := uniqueSorted(options.ExpectedPolicyVersions)
	promptVersions := make(map[string]string, len(options.ExpectedPromptVersions))
	for agent, version := range options.ExpectedPromptVersions {
		if strings.TrimSpace(agent) == "" || strings.TrimSpace(version) == "" {
			return nil, fmt.Errorf("expected prompt binding is invalid")
		}
		promptVersions[agent] = version
	}
	return &ResumeValidator{gitBinary: options.GitBinary, expectedPolicyVersions: policyVersions, expectedPromptVersions: promptVersions}, nil
}

func (v *ResumeValidator) Capture(state *domain.RunState) domain.ResumeGuard {
	guard := domain.ResumeGuard{CapturedAt: time.Now().UTC(), PolicyVersions: []string{}, PromptBindings: []string{}}
	if state.Workspace != nil {
		guard.WorkspaceID = state.Workspace.ID
		guard.WorkspacePath = state.Workspace.Path
		guard.BaseCommit = state.Workspace.BaseCommit
	}
	policies := make([]string, 0)
	for _, call := range state.ToolCallAudits {
		if call.PolicyVersion != "" {
			policies = append(policies, call.PolicyVersion)
		}
	}
	bindings := make([]string, 0, len(state.ModelInvocations))
	for _, call := range state.ModelInvocations {
		bindings = append(bindings, call.Agent+"|"+call.PromptVersion+"|"+call.PromptSHA256)
	}
	if state.PendingApproval != nil {
		guard.ApprovalID = state.PendingApproval.ApprovalID
		guard.ApprovalInputSHA = state.PendingApproval.InputSHA256
		guard.ApprovalPolicy = state.PendingApproval.PolicyVersion
		if guard.ApprovalPolicy != "" {
			policies = append(policies, guard.ApprovalPolicy)
		}
	}
	guard.PolicyVersions = uniqueSorted(policies)
	guard.PromptBindings = uniqueSorted(bindings)
	return guard
}

func (v *ResumeValidator) Validate(ctx context.Context, state *domain.RunState) error {
	if state == nil || state.ResumeGuard == nil {
		return fmt.Errorf("paused run has no resume compatibility guard")
	}
	expected := *state.ResumeGuard
	current := v.Capture(state)
	current.CapturedAt = expected.CapturedAt
	if expected.WorkspaceID != current.WorkspaceID || expected.WorkspacePath != current.WorkspacePath || expected.BaseCommit != current.BaseCommit ||
		expected.ApprovalID != current.ApprovalID || expected.ApprovalInputSHA != current.ApprovalInputSHA || expected.ApprovalPolicy != current.ApprovalPolicy ||
		!slices.Equal(expected.PolicyVersions, current.PolicyVersions) || !slices.Equal(expected.PromptBindings, current.PromptBindings) {
		return fmt.Errorf("run state, prompt, policy, workspace, or approval changed while paused")
	}
	for _, required := range v.expectedPolicyVersions {
		if _, exists := slices.BinarySearch(expected.PolicyVersions, required); !exists {
			return fmt.Errorf("required policy version %q is incompatible with paused run", required)
		}
	}
	for agent, version := range v.expectedPromptVersions {
		prefix := agent + "|" + version + "|"
		found := false
		for _, binding := range expected.PromptBindings {
			if strings.HasPrefix(binding, prefix) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("prompt version for %q is incompatible with paused run", agent)
		}
	}
	if expected.WorkspacePath == "" {
		return nil
	}
	info, err := os.Stat(expected.WorkspacePath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("paused workspace is missing")
	}
	commandContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandContext, v.gitBinary, "-C", expected.WorkspacePath, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("validate paused workspace base commit: %w", err)
	}
	if strings.TrimSpace(string(output)) != expected.BaseCommit {
		return fmt.Errorf("paused workspace base commit changed")
	}
	return nil
}

func uniqueSorted(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	result = slices.Compact(result)
	if result == nil {
		return []string{}
	}
	return result
}

var _ Validator = (*ResumeValidator)(nil)
