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
	CurrentPolicyVersion   string
	ExpectedPromptVersions map[string]string
	ExpectedPromptSHA256   map[string]string
	ExpectedModelVersions  map[string]string
	ExpectedToolVersions   map[string]string
}

type ResumeValidator struct {
	gitBinary              string
	expectedPolicyVersions []string
	currentPolicyVersion   string
	expectedPromptVersions map[string]string
	expectedPromptSHA256   map[string]string
	expectedModelVersions  map[string]string
	expectedToolVersions   map[string]string
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
	promptSHA256, err := normalizedBindings(options.ExpectedPromptSHA256, "prompt SHA")
	if err != nil {
		return nil, err
	}
	modelVersions, err := normalizedBindings(options.ExpectedModelVersions, "model")
	if err != nil {
		return nil, err
	}
	toolVersions, err := normalizedBindings(options.ExpectedToolVersions, "tool")
	if err != nil {
		return nil, err
	}
	return &ResumeValidator{
		gitBinary: options.GitBinary, expectedPolicyVersions: policyVersions,
		currentPolicyVersion:   strings.TrimSpace(options.CurrentPolicyVersion),
		expectedPromptVersions: promptVersions, expectedPromptSHA256: promptSHA256,
		expectedModelVersions: modelVersions, expectedToolVersions: toolVersions,
	}, nil
}

func (v *ResumeValidator) Capture(state *domain.RunState) domain.ResumeGuard {
	guard := domain.ResumeGuard{CapturedAt: time.Now().UTC(), PolicyVersions: []string{}, PromptBindings: []string{}, ModelBindings: []string{}, ToolBindings: []string{}}
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
		if call.ToolName != "" && call.ToolVersion != "" {
			guard.ToolBindings = append(guard.ToolBindings, call.ToolName+"|"+call.ToolVersion)
		}
	}
	bindings := make([]string, 0, len(state.ModelInvocations))
	for _, call := range state.ModelInvocations {
		bindings = append(bindings, call.Agent+"|"+call.PromptVersion+"|"+call.PromptSHA256)
		if call.Agent != "" && call.Model != "" {
			guard.ModelBindings = append(guard.ModelBindings, call.Agent+"|"+call.Model)
		}
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
	guard.ModelBindings = uniqueSorted(guard.ModelBindings)
	guard.ToolBindings = uniqueSorted(guard.ToolBindings)
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
		!slices.Equal(expected.PolicyVersions, current.PolicyVersions) || !slices.Equal(expected.PromptBindings, current.PromptBindings) ||
		!slices.Equal(expected.ModelBindings, current.ModelBindings) || !slices.Equal(expected.ToolBindings, current.ToolBindings) {
		return fmt.Errorf("run state, prompt, model, policy, tool, workspace, or approval changed while paused")
	}
	for _, required := range v.expectedPolicyVersions {
		if _, exists := slices.BinarySearch(expected.PolicyVersions, required); !exists {
			return fmt.Errorf("required policy version %q is incompatible with paused run", required)
		}
	}
	for _, binding := range expected.PromptBindings {
		parts := strings.Split(binding, "|")
		if len(parts) != 3 {
			return fmt.Errorf("prompt binding %q is invalid", binding)
		}
		version, configured := v.expectedPromptVersions[parts[0]]
		if len(v.expectedPromptVersions) > 0 && (!configured || parts[1] != version || (v.expectedPromptSHA256[parts[0]] != "" && parts[2] != v.expectedPromptSHA256[parts[0]])) {
			return fmt.Errorf("prompt binding for %q is incompatible with paused run", parts[0])
		}
	}
	if err := validateVersionBindings("model", expected.ModelBindings, v.expectedModelVersions); err != nil {
		return err
	}
	if err := validateVersionBindings("tool", expected.ToolBindings, v.expectedToolVersions); err != nil {
		return err
	}
	if v.currentPolicyVersion != "" {
		for _, version := range expected.PolicyVersions {
			if version != v.currentPolicyVersion {
				return fmt.Errorf("policy version %q is incompatible with current worker", version)
			}
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

func normalizedBindings(values map[string]string, kind string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for name, version := range values {
		name, version = strings.TrimSpace(name), strings.TrimSpace(version)
		if name == "" || version == "" {
			return nil, fmt.Errorf("expected %s binding is invalid", kind)
		}
		result[name] = version
	}
	return result, nil
}

func validateVersionBindings(kind string, bindings []string, expected map[string]string) error {
	for _, binding := range bindings {
		parts := strings.Split(binding, "|")
		if len(parts) != 2 {
			return fmt.Errorf("%s binding %q is invalid", kind, binding)
		}
		version, configured := expected[parts[0]]
		if len(expected) > 0 && (!configured || version != parts[1]) {
			return fmt.Errorf("%s version for %q is incompatible with paused run", kind, parts[0])
		}
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
