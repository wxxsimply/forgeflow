package policy

import (
	"context"
	"path"
	"regexp"
	"slices"
	"strings"

	"forgeflow/internal/domain"
)

type Action string

const (
	ActionAllow           Action = "allow"
	ActionDeny            Action = "deny"
	ActionRequireApproval Action = "require_approval"
)

type Phase string

const (
	PhaseBefore Phase = "before"
	PhaseAfter  Phase = "after"
)

type Command struct {
	Program    string
	Args       []string
	WorkingDir string
	EnvAllow   []string
}

type Metadata struct {
	Paths          []string
	Command        *Command
	NetworkTargets []string
}

type ApprovalEvidence struct {
	Approved      bool
	ToolName      string
	ToolVersion   string
	InputSHA256   string
	WorkspaceID   string
	PolicyVersion string
}

type Request struct {
	Phase         Phase
	RunID         string
	Agent         string
	ToolName      string
	ToolVersion   string
	Risk          domain.RiskLevel
	InputSHA256   string
	WorkspaceID   string
	WorkspacePath string
	Metadata      Metadata
	Budget        domain.RunBudget
	OutputBytes   int64
	Approval      *ApprovalEvidence
}

type Decision struct {
	Action        Action           `json:"action"`
	Code          string           `json:"code"`
	Reason        string           `json:"reason"`
	RuleID        string           `json:"ruleId"`
	PolicyVersion string           `json:"policyVersion"`
	Risk          domain.RiskLevel `json:"risk"`
}

type Engine interface {
	Version() string
	Evaluate(context.Context, Request) (Decision, error)
}

type CommandRule struct {
	Program      string
	ArgsPrefixes [][]string
	AllowedEnv   []string
}

type Rule struct {
	ID              string
	ToolName        string
	AllowedAgents   []string
	Action          Action
	Risk            domain.RiskLevel
	AllowNetwork    bool
	AllowedCommands []CommandRule
}

type StaticEngine struct {
	version string
	rules   map[string]Rule
}

func NewStaticEngine(version string, rules []Rule) (*StaticEngine, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, policyConfigError("policy version is required")
	}
	indexed := make(map[string]Rule, len(rules))
	for _, rule := range rules {
		if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.ToolName) == "" {
			return nil, policyConfigError("policy rule id and tool name are required")
		}
		if rule.Action != ActionAllow && rule.Action != ActionDeny && rule.Action != ActionRequireApproval {
			return nil, policyConfigError("policy rule has an invalid action")
		}
		if _, duplicate := indexed[rule.ToolName]; duplicate {
			return nil, policyConfigError("policy contains duplicate tool rules")
		}
		indexed[rule.ToolName] = rule
	}
	return &StaticEngine{version: version, rules: indexed}, nil
}

func DefaultEngine() *StaticEngine {
	engine, err := NewStaticEngine("policy/v1", []Rule{
		{ID: "repo-read", ToolName: "list_files", AllowedAgents: standardAgents(), Action: ActionAllow, Risk: domain.RiskLow},
		{ID: "repo-search", ToolName: "search_code", AllowedAgents: standardAgents(), Action: ActionAllow, Risk: domain.RiskLow},
		{ID: "repo-read-file", ToolName: "read_file", AllowedAgents: standardAgents(), Action: ActionAllow, Risk: domain.RiskLow},
		{ID: "repo-rules", ToolName: "read_project_rules", AllowedAgents: standardAgents(), Action: ActionAllow, Risk: domain.RiskLow},
		{ID: "repo-status", ToolName: "inspect_git_status", AllowedAgents: standardAgents(), Action: ActionAllow, Risk: domain.RiskLow},
		{ID: "test-command", ToolName: "run_test", AllowedAgents: []string{"developer", "tester"}, Action: ActionAllow, Risk: domain.RiskMedium,
			AllowedCommands: []CommandRule{
				{Program: "go", ArgsPrefixes: [][]string{{"test"}}, AllowedEnv: []string{"CI"}},
				{Program: "npm", ArgsPrefixes: [][]string{{"test"}, {"run", "test"}}, AllowedEnv: []string{"CI"}},
				{Program: "cargo", ArgsPrefixes: [][]string{{"test"}}, AllowedEnv: []string{"CI"}},
				{Program: "python", ArgsPrefixes: [][]string{{"-m", "pytest"}}, AllowedEnv: []string{"CI"}},
			}},
		{ID: "static-command", ToolName: "run_static_check", AllowedAgents: []string{"developer", "tester", "reviewer", "security"}, Action: ActionAllow, Risk: domain.RiskMedium,
			AllowedCommands: []CommandRule{
				{Program: "go", ArgsPrefixes: [][]string{{"vet"}}, AllowedEnv: []string{"CI"}},
				{Program: "golangci-lint", ArgsPrefixes: [][]string{{"run"}}, AllowedEnv: []string{"CI"}},
				{Program: "npm", ArgsPrefixes: [][]string{{"run", "lint"}}, AllowedEnv: []string{"CI"}},
				{Program: "cargo", ArgsPrefixes: [][]string{{"clippy"}}, AllowedEnv: []string{"CI"}},
			}},
		{ID: "patch-approval", ToolName: "apply_patch", AllowedAgents: []string{"developer"}, Action: ActionRequireApproval, Risk: domain.RiskHigh},
	})
	if err != nil {
		panic(err)
	}
	return engine
}

func (e *StaticEngine) Version() string { return e.version }

func (e *StaticEngine) Evaluate(ctx context.Context, request Request) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	rule, exists := e.rules[request.ToolName]
	if !exists {
		return e.deny(request, "unknown_tool", "tool is not present in the policy", "default-deny"), nil
	}
	if request.ToolVersion == "" || request.WorkspaceID == "" || request.WorkspacePath == "" || request.InputSHA256 == "" {
		return e.deny(request, "incomplete_context", "tool policy context is incomplete", rule.ID), nil
	}
	if !slices.Contains(rule.AllowedAgents, request.Agent) {
		return e.deny(request, "agent_not_allowed", "agent is not allowed to use this tool", rule.ID), nil
	}
	if len(request.Metadata.NetworkTargets) > 0 && !rule.AllowNetwork {
		return e.deny(request, "network_denied", "network access is disabled for this tool", rule.ID), nil
	}
	for _, candidate := range request.Metadata.Paths {
		if !safeRelativePath(candidate) {
			return e.deny(request, "path_denied", "tool path must stay within the workspace", rule.ID), nil
		}
	}
	if request.Metadata.Command != nil {
		if decision := e.validateCommand(request, rule); decision != nil {
			return *decision, nil
		}
	}
	if request.Phase == PhaseBefore {
		if request.Approval == nil || !request.Approval.Approved {
			if allowed, reason := request.Budget.ToolCallAllowed(); !allowed {
				return e.deny(request, "budget_exhausted", reason, rule.ID), nil
			}
		}
		if rule.Action == ActionRequireApproval {
			if request.Approval == nil {
				return Decision{Action: ActionRequireApproval, Code: "approval_required", Reason: "tool requires explicit approval", RuleID: rule.ID, PolicyVersion: e.version, Risk: rule.Risk}, nil
			}
			if !approvalMatches(request, *request.Approval, e.version) {
				return e.deny(request, "approval_mismatch", "approval does not match the tool request", rule.ID), nil
			}
		}
	}
	if request.Phase == PhaseAfter && request.Budget.MaxToolOutputBytes > 0 && request.Budget.ToolOutputBytes+request.OutputBytes > request.Budget.MaxToolOutputBytes {
		return e.deny(request, "budget_exhausted", "tool output byte budget exceeded", rule.ID), nil
	}
	if rule.Action == ActionDeny {
		return e.deny(request, "rule_denied", "tool is denied by policy", rule.ID), nil
	}
	return Decision{Action: ActionAllow, Code: "allowed", Reason: "tool request satisfies policy", RuleID: rule.ID, PolicyVersion: e.version, Risk: maxRisk(request.Risk, rule.Risk)}, nil
}

func (e *StaticEngine) validateCommand(request Request, rule Rule) *Decision {
	command := request.Metadata.Command
	if command == nil || !safeProgram(command.Program) || !safeRelativePath(command.WorkingDir) {
		decision := e.deny(request, "command_denied", "command program or working directory is invalid", rule.ID)
		return &decision
	}
	for _, argument := range command.Args {
		if unsafeArgument(argument) {
			decision := e.deny(request, "command_denied", "command argument contains forbidden shell syntax", rule.ID)
			return &decision
		}
	}
	for _, allowed := range rule.AllowedCommands {
		if command.Program != allowed.Program || !matchesAnyPrefix(command.Args, allowed.ArgsPrefixes) {
			continue
		}
		for _, name := range command.EnvAllow {
			if !slices.Contains(allowed.AllowedEnv, name) {
				decision := e.deny(request, "environment_denied", "command requested a non-allowlisted environment variable", rule.ID)
				return &decision
			}
		}
		return nil
	}
	decision := e.deny(request, "command_denied", "command is not allowlisted for this tool", rule.ID)
	return &decision
}

func (e *StaticEngine) deny(request Request, code, reason, ruleID string) Decision {
	return Decision{Action: ActionDeny, Code: code, Reason: reason, RuleID: ruleID, PolicyVersion: e.version, Risk: request.Risk}
}

func approvalMatches(request Request, approval ApprovalEvidence, version string) bool {
	return approval.Approved && approval.ToolName == request.ToolName && approval.ToolVersion == request.ToolVersion &&
		approval.InputSHA256 == request.InputSHA256 && approval.WorkspaceID == request.WorkspaceID && approval.PolicyVersion == version
}

func safeRelativePath(value string) bool {
	if value == "" {
		value = "."
	}
	if strings.ContainsAny(value, "\x00\\\r\n") || strings.HasPrefix(value, "/") || windowsVolumePattern.MatchString(value) {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

var windowsVolumePattern = regexp.MustCompile(`^[A-Za-z]:`)
var programPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

func safeProgram(program string) bool {
	if !programPattern.MatchString(program) {
		return false
	}
	switch strings.ToLower(program) {
	case "sh", "bash", "zsh", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh":
		return false
	default:
		return true
	}
}

func unsafeArgument(argument string) bool {
	return strings.ContainsAny(argument, "\x00\r\n|;&><`") || strings.Contains(argument, "$(") || strings.Contains(argument, "${")
}

func matchesAnyPrefix(arguments []string, prefixes [][]string) bool {
	for _, prefix := range prefixes {
		if len(arguments) < len(prefix) {
			continue
		}
		matched := true
		for index := range prefix {
			if arguments[index] != prefix[index] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func standardAgents() []string {
	return []string{"planner", "developer", "tester", "reviewer", "security"}
}

func maxRisk(left, right domain.RiskLevel) domain.RiskLevel {
	if left == domain.RiskHigh || right == domain.RiskHigh {
		return domain.RiskHigh
	}
	if left == domain.RiskMedium || right == domain.RiskMedium {
		return domain.RiskMedium
	}
	return domain.RiskLow
}
