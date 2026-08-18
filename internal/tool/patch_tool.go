package tool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/policy"
)

type PatchLimits struct {
	MaxPatchBytes   int
	MaxFiles        int
	MaxChangedLines int
	ProtectedPaths  []string
}

func DefaultPatchLimits() PatchLimits {
	return PatchLimits{
		MaxPatchBytes: 192 * 1024, MaxFiles: 32, MaxChangedLines: 2_000,
		ProtectedPaths: []string{
			".git", ".forgeflow", ".github/workflows", ".env", "AGENTS.md",
			"configs/policies", "evals/hidden",
			"internal/planner/prompts", "internal/developer/prompts",
			"internal/reviewer/prompts", "internal/security/prompts",
			"internal/policy", "internal/judge", "internal/security",
			"internal/governance", "internal/eval",
		},
	}
}

type ApplyPatchInput struct {
	Patch         string   `json:"patch"`
	ExpectedFiles []string `json:"expectedFiles"`
}

type ApplyPatchOutput struct {
	AppliedFiles []string `json:"appliedFiles"`
	ChangedLines int      `json:"changedLines"`
	PatchSHA256  string   `json:"patchSha256"`
}

type patchTool struct{ limits PatchLimits }

const applyPatchInputSchema = `{"type":"object","properties":{"patch":{"type":"string"},"expectedFiles":{"type":"array","minItems":1,"items":{"type":"string"}}},"required":["patch","expectedFiles"],"additionalProperties":false}`
const applyPatchOutputSchema = `{"type":"object","properties":{"appliedFiles":{"type":"array","items":{"type":"string"}},"changedLines":{"type":"integer"},"patchSha256":{"type":"string"}},"required":["appliedFiles","changedLines","patchSha256"],"additionalProperties":false}`

func RegisterMutationTools(registry *Registry, limits PatchLimits) error {
	if registry == nil {
		return apperror.New(apperror.CodeValidation, "mutation tool registry is required")
	}
	limits = normalizePatchLimits(limits)
	return registry.Register(&patchTool{limits: limits})
}

func (t *patchTool) Spec() Spec {
	return Spec{
		Name: "apply_patch", Version: "v1", Description: "Apply one bounded unified Git patch inside the authorized worktree paths.",
		InputSchema: json.RawMessage(applyPatchInputSchema), OutputSchema: json.RawMessage(applyPatchOutputSchema),
		Risk: domain.RiskHigh, Timeout: 30 * time.Second, MaxOutputBytes: 64 * 1024,
	}
}

func (t *patchTool) Analyze(input json.RawMessage) (policy.Metadata, error) {
	request, paths, _, err := t.decode(input)
	_ = request
	return policy.Metadata{Paths: paths}, err
}

func (t *patchTool) Execute(ctx context.Context, call CallContext, input json.RawMessage) (json.RawMessage, error) {
	request, paths, changedLines, err := t.decode(input)
	if err != nil {
		return nil, err
	}
	if err := validatePatchAuthorization(paths, call.AllowedPaths, t.limits.ProtectedPaths); err != nil {
		return nil, err
	}
	workspace, err := canonicalWorkspace(call.Workspace.Path)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if err := validatePatchTarget(workspace, path); err != nil {
			return nil, err
		}
	}
	patch := []byte(request.Patch)
	if err := runGitApply(ctx, workspace, patch, true); err != nil {
		return nil, err
	}
	if err := runGitApply(ctx, workspace, patch, false); err != nil {
		return nil, err
	}
	for _, path := range paths {
		if err := validatePatchTarget(workspace, path); err != nil {
			return nil, apperror.Wrap(err, apperror.CodePolicyDenied, "tool.patch.verify", "applied patch produced an unsafe path")
		}
	}
	digest := sha256.Sum256(patch)
	output, err := json.Marshal(ApplyPatchOutput{
		AppliedFiles: paths, ChangedLines: changedLines, PatchSHA256: hex.EncodeToString(digest[:]),
	})
	if err != nil {
		return nil, apperror.Wrap(err, apperror.CodeInternal, "tool.patch.output", "patch result could not be encoded")
	}
	return output, nil
}

func (t *patchTool) ValidateOutput(output json.RawMessage) error {
	var result ApplyPatchOutput
	if err := decodeStrict(output, &result); err != nil {
		return err
	}
	if len(result.AppliedFiles) == 0 || result.ChangedLines <= 0 || len(result.PatchSHA256) != 64 {
		return fmt.Errorf("patch output is incomplete")
	}
	return nil
}

func (t *patchTool) decode(input json.RawMessage) (ApplyPatchInput, []string, int, error) {
	var request ApplyPatchInput
	if err := decodeStrict(input, &request); err != nil {
		return request, nil, 0, err
	}
	if len(request.Patch) == 0 || len(request.Patch) > t.limits.MaxPatchBytes {
		return request, nil, 0, apperror.New(apperror.CodeBudget, "patch is empty or exceeds the byte limit")
	}
	paths, changedLines, err := parseUnifiedPatch(request.Patch)
	if err != nil {
		return request, nil, 0, err
	}
	if len(paths) > t.limits.MaxFiles || changedLines > t.limits.MaxChangedLines {
		return request, nil, 0, apperror.New(apperror.CodeBudget, "patch exceeds the file or changed-line limit")
	}
	for _, candidate := range paths {
		if protectedPatchPath(candidate, t.limits.ProtectedPaths) {
			return request, nil, 0, apperror.New(apperror.CodePolicyDenied, "patch targets a protected governance or secret path")
		}
	}
	expected := append([]string(nil), request.ExpectedFiles...)
	for _, file := range expected {
		if !safePatchPath(file) {
			return request, nil, 0, apperror.New(apperror.CodeValidation, "expected patch file is not a normalized relative path")
		}
	}
	slices.Sort(expected)
	if len(expected) != len(paths) || !slices.Equal(expected, paths) || len(slices.Compact(expected)) != len(expected) {
		return request, nil, 0, apperror.New(apperror.CodePolicyDenied, "declared changedFiles does not match the patch headers")
	}
	return request, paths, changedLines, nil
}

func parseUnifiedPatch(patch string) ([]string, int, error) {
	if strings.ContainsRune(patch, 0) || strings.Contains(patch, "GIT binary patch") || strings.Contains(patch, "new file mode 120000") || strings.Contains(patch, "new mode 120000") {
		return nil, 0, apperror.New(apperror.CodePolicyDenied, "binary patches and symbolic links are not allowed")
	}
	paths := make(map[string]struct{})
	changedLines := 0
	inHeader := false
	inHunk := false
	for _, line := range strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			fields := strings.Fields(line)
			if len(fields) != 4 {
				return nil, 0, apperror.New(apperror.CodeValidation, "patch contains an unsupported diff header")
			}
			left, leftOK := trimDiffPrefix(fields[2], "a/")
			right, rightOK := trimDiffPrefix(fields[3], "b/")
			if !leftOK || !rightOK || left != right || !safePatchPath(left) {
				return nil, 0, apperror.New(apperror.CodePolicyDenied, "patch path is unsafe or attempts a rename")
			}
			paths[left] = struct{}{}
			inHeader, inHunk = true, false
		case inHeader && (strings.HasPrefix(line, "rename from ") || strings.HasPrefix(line, "rename to ") || strings.HasPrefix(line, "copy from ") || strings.HasPrefix(line, "copy to ")):
			return nil, 0, apperror.New(apperror.CodePolicyDenied, "patch rename and copy operations are not allowed")
		case strings.HasPrefix(line, "@@"):
			if !inHeader {
				return nil, 0, apperror.New(apperror.CodeValidation, "patch hunk appears before a file header")
			}
			inHunk = true
		case inHunk && len(line) > 0 && (line[0] == '+' || line[0] == '-'):
			changedLines++
		}
	}
	if len(paths) == 0 || changedLines == 0 {
		return nil, 0, apperror.New(apperror.CodeValidation, "patch must contain at least one text change")
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, changedLines, nil
}

func trimDiffPrefix(value, prefix string) (string, bool) {
	if !strings.HasPrefix(value, prefix) || strings.ContainsAny(value, "\"'\t\r\n") {
		return "", false
	}
	return strings.TrimPrefix(value, prefix), true
}

func safePatchPath(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\\\r\n") {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func validatePatchAuthorization(paths, allowed, protected []string) error {
	authorized := make(map[string]struct{}, len(allowed))
	for _, path := range allowed {
		if safePatchPath(path) {
			authorized[path] = struct{}{}
		}
	}
	for _, candidate := range paths {
		if _, exists := authorized[candidate]; !exists {
			return apperror.New(apperror.CodePolicyDenied, "patch changes a file outside the approved plan")
		}
		if protectedPatchPath(candidate, protected) {
			return apperror.New(apperror.CodePolicyDenied, "patch targets a protected governance or secret path")
		}
	}
	return nil
}

func protectedPatchPath(candidate string, protected []string) bool {
	lower := strings.ToLower(candidate)
	for _, protectedPath := range protected {
		protectedLower := strings.ToLower(strings.TrimSuffix(filepath.ToSlash(protectedPath), "/"))
		if lower == protectedLower || strings.HasPrefix(lower, protectedLower+"/") || (protectedLower == ".env" && strings.HasPrefix(lower, ".env.")) || (protectedLower == "agents.md" && strings.EqualFold(filepath.Base(candidate), "AGENTS.md")) {
			return true
		}
	}
	return false
}

func canonicalWorkspace(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", apperror.Wrap(err, apperror.CodeValidation, "tool.patch.workspace", "workspace path is invalid")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", apperror.Wrap(err, apperror.CodeValidation, "tool.patch.workspace", "workspace path is inaccessible")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", apperror.New(apperror.CodeValidation, "workspace must be an accessible directory")
	}
	return filepath.Clean(resolved), nil
}

func validatePatchTarget(workspace, relative string) error {
	target := filepath.Join(workspace, filepath.FromSlash(relative))
	resolved := target
	if _, err := os.Lstat(target); err == nil {
		resolved, err = filepath.EvalSymlinks(target)
		if err != nil {
			return apperror.Wrap(err, apperror.CodePolicyDenied, "tool.patch.path", "patch target symbolic link is invalid")
		}
	} else if os.IsNotExist(err) {
		ancestor := filepath.Dir(target)
		for {
			parent, parentErr := filepath.EvalSymlinks(ancestor)
			if parentErr == nil {
				remainder, relativeErr := filepath.Rel(ancestor, target)
				if relativeErr != nil {
					return apperror.Wrap(relativeErr, apperror.CodePolicyDenied, "tool.patch.path", "new patch target path is invalid")
				}
				resolved = filepath.Join(parent, remainder)
				break
			}
			if !os.IsNotExist(parentErr) || ancestor == workspace || filepath.Dir(ancestor) == ancestor {
				return apperror.Wrap(parentErr, apperror.CodePolicyDenied, "tool.patch.path", "new patch target parent is inaccessible")
			}
			ancestor = filepath.Dir(ancestor)
		}
	} else {
		return apperror.Wrap(err, apperror.CodePolicyDenied, "tool.patch.path", "patch target is inaccessible")
	}
	relativeToRoot, err := filepath.Rel(workspace, resolved)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relativeToRoot) {
		return apperror.New(apperror.CodePolicyDenied, "patch target escapes the workspace")
	}
	return nil
}

func runGitApply(ctx context.Context, workspace string, patch []byte, check bool) error {
	args := []string{"--no-optional-locks", "-c", "core.hooksPath=" + os.DevNull, "-C", workspace, "apply", "--whitespace=nowarn"}
	if check {
		args = append(args, "--check")
	}
	args = append(args, "-")
	command := exec.CommandContext(ctx, "git", args...)
	command.Stdin = bytes.NewReader(patch)
	stderr := &boundedErrorBuffer{limit: 64 * 1024}
	command.Stdout = &boundedErrorBuffer{limit: 4 * 1024}
	command.Stderr = stderr
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C")
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return apperror.Wrap(ctx.Err(), apperror.CodeTimeout, "tool.patch.git", "git apply timed out or was cancelled")
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return apperror.New(apperror.CodeValidation, "git apply rejected the patch: "+message)
	}
	return nil
}

type boundedErrorBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedErrorBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.buffer.Write(data)
	}
	return original, nil
}

func (b *boundedErrorBuffer) String() string { return b.buffer.String() }

func normalizePatchLimits(limits PatchLimits) PatchLimits {
	defaults := DefaultPatchLimits()
	if limits.MaxPatchBytes <= 0 {
		limits.MaxPatchBytes = defaults.MaxPatchBytes
	}
	if limits.MaxFiles <= 0 {
		limits.MaxFiles = defaults.MaxFiles
	}
	if limits.MaxChangedLines <= 0 {
		limits.MaxChangedLines = defaults.MaxChangedLines
	}
	if len(limits.ProtectedPaths) == 0 {
		limits.ProtectedPaths = defaults.ProtectedPaths
	}
	return limits
}

var _ Tool = (*patchTool)(nil)
