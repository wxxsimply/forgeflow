package security

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/assessment"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
	"forgeflow/internal/repository"
)

type Security interface {
	Name() string
	Version() string
	Execute(context.Context, assessment.Input) (Result, error)
}

type Result struct {
	Security   domain.SecurityResult
	Invocation *domain.ModelInvocation
}

type Fake struct {
	mu      sync.Mutex
	Results []Result
	Errors  []error
	Inputs  []assessment.Input
	calls   int
}

func (*Fake) Name() string    { return "security" }
func (*Fake) Version() string { return "fake/v1" }

func (f *Fake) Execute(_ context.Context, input assessment.Input) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Inputs = append(f.Inputs, input)
	index := f.calls
	f.calls++
	if index < len(f.Errors) && f.Errors[index] != nil {
		return Result{}, f.Errors[index]
	}
	if index >= len(f.Results) {
		return Result{}, apperror.New(apperror.CodeModelOutput, "fake security agent has no configured result")
	}
	return f.Results[index], nil
}

func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type Options struct {
	Provider        model.Provider
	ContextBuilder  *assessment.ContextBuilder
	PromptLoader    *PromptLoader
	Model           string
	PromptVersion   string
	ReasoningEffort string
	MaxOutputTokens int
	Timeout         time.Duration
	Pricing         model.Pricing
}

type Agent struct {
	provider        model.Provider
	contextBuilder  *assessment.ContextBuilder
	prompts         *PromptLoader
	model           string
	promptVersion   string
	reasoningEffort string
	maxOutputTokens int
	timeout         time.Duration
	pricing         model.Pricing
}

func NewAgent(options Options) (*Agent, error) {
	if options.Provider == nil {
		return nil, apperror.New(apperror.CodeValidation, "security model provider is required")
	}
	if options.ContextBuilder == nil {
		options.ContextBuilder = assessment.NewContextBuilder(repository.DefaultLimits(), 512*1024)
	}
	if options.PromptLoader == nil {
		options.PromptLoader = NewPromptLoader(nil)
	}
	if options.Model == "" {
		options.Model = "gpt-5.6"
	}
	if options.PromptVersion == "" {
		options.PromptVersion = "security/v1"
	}
	if options.MaxOutputTokens <= 0 {
		options.MaxOutputTokens = 8_000
	}
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Minute
	}
	if options.MaxOutputTokens > 128_000 || options.Timeout > 10*time.Minute {
		return nil, apperror.New(apperror.CodeValidation, "security output or timeout limit is invalid")
	}
	if err := options.Pricing.Validate(); err != nil {
		return nil, apperror.Wrap(err, apperror.CodeValidation, "security.pricing", "security pricing is invalid")
	}
	if _, err := options.PromptLoader.Load(options.PromptVersion); err != nil {
		return nil, err
	}
	return &Agent{
		provider: options.Provider, contextBuilder: options.ContextBuilder, prompts: options.PromptLoader,
		model: options.Model, promptVersion: options.PromptVersion, reasoningEffort: options.ReasoningEffort,
		maxOutputTokens: options.MaxOutputTokens, timeout: options.Timeout, pricing: options.Pricing,
	}, nil
}

func (*Agent) Name() string             { return "security" }
func (*Agent) Version() string          { return "v1" }
func (a *Agent) Timeout() time.Duration { return a.timeout }

func (a *Agent) Execute(ctx context.Context, input assessment.Input) (Result, error) {
	if allowed, reason := input.Budget.ModelCallAllowed(); !allowed {
		return Result{}, apperror.New(apperror.CodeBudget, reason)
	}
	bundle, err := a.contextBuilder.Build(ctx, input)
	if err != nil {
		return Result{}, err
	}
	prompt, err := a.prompts.Load(a.promptVersion)
	if err != nil {
		return Result{}, err
	}
	userPrompt, err := prompt.Render(bundle)
	if err != nil {
		return Result{}, err
	}
	callContext, cancel := context.WithTimeout(ctx, a.timeout)
	startedAt := time.Now().UTC()
	response, providerErr := model.GenerateObserved(callContext, a.provider, model.Request{
		Model: a.model, Instructions: prompt.System, Input: userPrompt,
		MaxOutputTokens: a.maxOutputTokens, ReasoningEffort: a.reasoningEffort,
		ResponseFormat: model.ResponseFormat{Name: "security_result", Description: "Independent security findings", Schema: SecurityResultSchema(), Strict: true},
	}, "security", "security", a.pricing)
	cancel()
	invocation := invocationFor(input, a, prompt, response, startedAt)
	if providerErr != nil {
		invocation.Status = "failed"
		return Result{Invocation: &invocation}, mapProviderError(providerErr)
	}
	securityResult, err := DecodeSecurityResult(response.OutputText)
	if err != nil {
		invocation.Status = "invalid_output"
		return Result{Invocation: &invocation}, err
	}
	if err := validateFindingScope(securityResult, input.Diff.ChangedFiles); err != nil {
		invocation.Status = "invalid_output"
		return Result{Invocation: &invocation}, err
	}
	securityResult.Findings = MergeFindings(DeterministicFindings(input.Diff), securityResult.Findings)
	return Result{Security: securityResult, Invocation: &invocation}, nil
}

type scanRule struct {
	title          string
	severity       domain.FindingSeverity
	pattern        *regexp.Regexp
	impact         string
	recommendation string
}

var deterministicRules = []scanRule{
	{title: "Private key material added to source", severity: domain.SeverityCritical, pattern: regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`), impact: "A committed private key can allow unauthorized access and cannot be made secret again.", recommendation: "Remove the key, rotate it, and load credentials from an approved secret store."},
	{title: "AWS access key identifier added to source", severity: domain.SeverityHigh, pattern: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), impact: "The credential identifier may expose a usable cloud credential pair.", recommendation: "Remove and rotate the credential, then use workload identity or an approved secret store."},
	{title: "Hard-coded credential added to source", severity: domain.SeverityHigh, pattern: regexp.MustCompile(`(?i)(?:password|passwd|secret|api[_-]?key|access[_-]?token)\s*[:=]\s*["'][^"'${}]{8,}["']`), impact: "Anyone with source or artifact access may obtain the credential.", recommendation: "Read the credential from an approved secret provider and rotate the exposed value."},
	{title: "Shell command execution added", severity: domain.SeverityHigh, pattern: regexp.MustCompile(`(?:exec\.Command\s*\(\s*["'](?:sh|bash|cmd)["']\s*,\s*["'](?:-c|/c)["']|os\.system\s*\(|subprocess\.(?:run|Popen)\s*\([^\n]*shell\s*=\s*True)`), impact: "Untrusted input reaching a shell can lead to arbitrary command execution.", recommendation: "Avoid a shell, pass fixed arguments directly, and strictly validate any user-controlled values."},
	{title: "World-writable permission added", severity: domain.SeverityHigh, pattern: regexp.MustCompile(`(?i)(?:chmod\s+(?:0?777)|Chmod\s*\([^\n,]+,\s*0?777\b)`), impact: "World-writable files or directories can be modified by untrusted local users or processes.", recommendation: "Use the narrowest owner/group permission required by the application."},
	{title: "Unrestricted network range added", severity: domain.SeverityHigh, pattern: regexp.MustCompile(`\b0\.0\.0\.0/0\b|\b::/0\b`), impact: "The resource may become reachable from the entire internet.", recommendation: "Restrict the source range to explicitly approved networks."},
}

func DeterministicFindings(diff domain.DiffArtifact) []domain.SecurityFinding {
	file := ""
	targetLine := 0
	findings := make([]domain.SecurityFinding, 0)
	for _, line := range strings.Split(diff.Patch, "\n") {
		if strings.HasPrefix(line, "diff --git a/") {
			parts := strings.SplitN(line, " b/", 2)
			if len(parts) == 2 {
				file = strings.TrimSpace(parts[1])
			}
			targetLine = 0
			continue
		}
		if strings.HasPrefix(line, "@@ ") {
			targetLine = parseTargetLine(line)
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		isAdded := strings.HasPrefix(line, "+")
		if isAdded && domain.ValidRelativePath(file) {
			content := strings.TrimPrefix(line, "+")
			for ruleIndex, rule := range deterministicRules {
				if !rule.pattern.MatchString(content) {
					continue
				}
				findings = append(findings, domain.SecurityFinding{
					ID:       fmt.Sprintf("SEC-DET-%02d-%s-%d", ruleIndex+1, shortDigest(file), targetLine),
					Severity: rule.severity, File: file, Line: targetLine, Title: rule.title,
					Evidence: "A matching security-sensitive pattern occurs on an added diff line.",
					Impact:   rule.impact, Recommendation: rule.recommendation, Confirmed: true,
				})
			}
		}
		if !strings.HasPrefix(line, "-") {
			targetLine++
		}
	}
	return MergeFindings(nil, findings)
}

func parseTargetLine(header string) int {
	match := regexp.MustCompile(`\+(\d+)`).FindStringSubmatch(header)
	if len(match) != 2 {
		return 0
	}
	var line int
	_, _ = fmt.Sscanf(match[1], "%d", &line)
	return line
}

func shortDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:4])
}

func MergeFindings(deterministic, generated []domain.SecurityFinding) []domain.SecurityFinding {
	seen := make(map[string]struct{}, len(deterministic)+len(generated))
	result := make([]domain.SecurityFinding, 0, len(deterministic)+len(generated))
	for _, finding := range append(append([]domain.SecurityFinding(nil), deterministic...), generated...) {
		if _, exists := seen[finding.ID]; exists {
			continue
		}
		seen[finding.ID] = struct{}{}
		result = append(result, finding)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func validateFindingScope(result domain.SecurityResult, changedFiles []string) error {
	allowed := make(map[string]struct{}, len(changedFiles))
	for _, file := range changedFiles {
		allowed[file] = struct{}{}
	}
	for _, finding := range result.Findings {
		if _, exists := allowed[finding.File]; !exists {
			return apperror.New(apperror.CodeModelOutput, "security finding references a file outside the final diff")
		}
	}
	return nil
}

//go:embed prompts/*/*
var embeddedPrompts embed.FS

var promptVersionPattern = regexp.MustCompile(`^security/v[1-9][0-9]*$`)

type Prompt struct{ Version, System, UserTemplate, SHA256 string }
type PromptLoader struct{ files fs.FS }

func NewPromptLoader(files fs.FS) *PromptLoader {
	if files == nil {
		files = embeddedPrompts
	}
	return &PromptLoader{files: files}
}

func (l *PromptLoader) Load(version string) (Prompt, error) {
	if !promptVersionPattern.MatchString(version) {
		return Prompt{}, apperror.New(apperror.CodeValidation, "security prompt version must look like security/v1")
	}
	system, err := fs.ReadFile(l.files, "prompts/"+version+"/system.txt")
	if err != nil {
		return Prompt{}, apperror.Wrap(err, apperror.CodeNotFound, "security.prompt.system", "security system prompt was not found")
	}
	user, err := fs.ReadFile(l.files, "prompts/"+version+"/user.tmpl")
	if err != nil {
		return Prompt{}, apperror.Wrap(err, apperror.CodeNotFound, "security.prompt.user", "security user prompt was not found")
	}
	digest := sha256.Sum256(append(append([]byte(nil), system...), user...))
	return Prompt{Version: version, System: strings.TrimSpace(string(system)), UserTemplate: string(user), SHA256: hex.EncodeToString(digest[:])}, nil
}

func (p Prompt) Render(bundle assessment.Bundle) (string, error) {
	tmpl, err := template.New(p.Version).Funcs(template.FuncMap{"jsonValue": func(value any) (string, error) {
		data, err := json.Marshal(value)
		return string(data), err
	}}).Option("missingkey=error").Parse(p.UserTemplate)
	if err != nil {
		return "", apperror.Wrap(err, apperror.CodeInternal, "security.prompt.parse", "security prompt is invalid")
	}
	var output strings.Builder
	if err := tmpl.Execute(&output, bundle); err != nil {
		return "", apperror.Wrap(err, apperror.CodeInternal, "security.prompt.render", "security prompt could not be rendered")
	}
	if output.Len() > 1024*1024 {
		return "", apperror.New(apperror.CodeBudget, "security prompt exceeds size limit")
	}
	return output.String(), nil
}

var securityResultSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["summary","findings"],"properties":{"summary":{"type":"string","minLength":1,"maxLength":4000},"findings":{"type":"array","maxItems":200,"items":{"type":"object","additionalProperties":false,"required":["id","severity","file","line","title","evidence","impact","recommendation","confirmed","humanReview"],"properties":{"id":{"type":"string"},"severity":{"type":"string","enum":["info","low","medium","high","critical"]},"file":{"type":"string"},"line":{"type":"integer","minimum":0},"title":{"type":"string"},"evidence":{"type":"string"},"impact":{"type":"string"},"recommendation":{"type":"string"},"confirmed":{"type":"boolean"},"humanReview":{"type":"boolean"}}}}}}`)

func SecurityResultSchema() json.RawMessage {
	return append(json.RawMessage(nil), securityResultSchema...)
}

type strictSecurity struct {
	Summary  *string                   `json:"summary"`
	Findings *[]domain.SecurityFinding `json:"findings"`
}

func DecodeSecurityResult(output string) (domain.SecurityResult, error) {
	if strings.TrimSpace(output) == "" || len(output) > 1024*1024 {
		return domain.SecurityResult{}, apperror.New(apperror.CodeModelOutput, "security agent returned empty or oversized output")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(output))
	decoder.DisallowUnknownFields()
	var decoded strictSecurity
	if err := decoder.Decode(&decoded); err != nil {
		return domain.SecurityResult{}, apperror.Wrap(err, apperror.CodeModelOutput, "security.decode", "security output is not strict JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return domain.SecurityResult{}, apperror.New(apperror.CodeModelOutput, "security output contains trailing data")
	}
	if decoded.Summary == nil || decoded.Findings == nil {
		return domain.SecurityResult{}, apperror.New(apperror.CodeModelOutput, "security output is missing fields")
	}
	result := domain.SecurityResult{Summary: *decoded.Summary, Findings: *decoded.Findings}
	if err := result.Validate(); err != nil {
		return domain.SecurityResult{}, apperror.Wrap(err, apperror.CodeModelOutput, "security.validate", "security output failed validation")
	}
	return result, nil
}

func invocationFor(input assessment.Input, agent *Agent, prompt Prompt, response model.Response, started time.Time) domain.ModelInvocation {
	modelName := response.Model
	if modelName == "" {
		modelName = agent.model
	}
	return domain.ModelInvocation{
		Agent: "security", AgentVersion: agent.Version(), NodeID: input.NodeID,
		Provider: agent.provider.Name(), Model: modelName, ResponseID: response.ID,
		PromptVersion: prompt.Version, PromptSHA256: prompt.SHA256, Status: response.Status,
		InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens,
		CachedInputTokens: response.Usage.CachedInputTokens, ReasoningTokens: response.Usage.ReasoningTokens,
		LatencyMilliseconds: time.Since(started).Milliseconds(), EstimatedCostUSD: agent.pricing.Estimate(response.Usage),
		PricingConfigured: agent.pricing.Configured(), CreatedAt: started,
	}
}

func mapProviderError(err error) error {
	var providerError *model.Error
	if !errors.As(err, &providerError) {
		return apperror.Wrap(err, apperror.CodeInternal, "security.provider", "model provider failed")
	}
	code := apperror.CodeInternal
	switch providerError.Kind {
	case model.ErrorAuthentication:
		code = apperror.CodeUnauthorized
	case model.ErrorRateLimit, model.ErrorTransient:
		code = apperror.CodeTransient
	case model.ErrorInvalidRequest:
		code = apperror.CodeValidation
	case model.ErrorRefusal:
		code = apperror.CodePolicyDenied
	case model.ErrorInvalidOutput:
		code = apperror.CodeModelOutput
	}
	return apperror.Wrap(err, code, "security.provider", providerError.Message)
}

var _ Security = (*Agent)(nil)
var _ Security = (*Fake)(nil)
