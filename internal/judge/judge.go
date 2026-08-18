package judge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"forgeflow/internal/domain"
	"forgeflow/internal/security"
)

const Version = "judge/v1"

type Input struct {
	Test             *domain.TestAssessment
	Review           *domain.ReviewResult
	Security         *domain.SecurityResult
	AssessmentErrors map[string]string
	Diff             *domain.DiffArtifact
	Plan             *domain.ExecutionPlan
	Budget           domain.RunBudget
	RepairCount      int
	Iteration        int
	WorkspaceID      string
}

type digestInput struct {
	Test             *domain.TestAssessment `json:"test"`
	Review           *domain.ReviewResult   `json:"review"`
	Security         *domain.SecurityResult `json:"security"`
	AssessmentErrors map[string]string      `json:"assessmentErrors"`
	Diff             *domain.DiffArtifact   `json:"diff"`
	Plan             *domain.ExecutionPlan  `json:"plan"`
	Budget           domain.RunBudget       `json:"budget"`
	RepairCount      int                    `json:"repairCount"`
	Iteration        int                    `json:"iteration"`
	WorkspaceID      string                 `json:"workspaceId"`
}

func Evaluate(input Input) domain.JudgeDecision {
	decision := domain.JudgeDecision{Version: Version, Action: domain.JudgePass, DecidedAt: time.Now().UTC()}
	decision.InputSHA256 = inputDigest(input)

	if len(input.AssessmentErrors) > 0 {
		keys := make([]string, 0, len(input.AssessmentErrors))
		for key := range input.AssessmentErrors {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			decision.Reasons = append(decision.Reasons, fmt.Sprintf("assessment %s failed: %s", key, input.AssessmentErrors[key]))
		}
		decision.Action = domain.JudgeFail
		return normalize(decision)
	}
	if input.Test == nil || input.Review == nil || input.Security == nil || input.Diff == nil || input.Plan == nil {
		decision.Action = domain.JudgeFail
		decision.Reasons = append(decision.Reasons, "judge evidence is incomplete")
		return normalize(decision)
	}
	if err := input.Review.Validate(); err != nil {
		decision.Action = domain.JudgeFail
		decision.Reasons = append(decision.Reasons, "review result is invalid: "+err.Error())
		return normalize(decision)
	}
	if err := input.Security.Validate(); err != nil {
		decision.Action = domain.JudgeFail
		decision.Reasons = append(decision.Reasons, "security result is invalid: "+err.Error())
		return normalize(decision)
	}

	deterministicSecurity := security.DeterministicFindings(*input.Diff)
	securityFindings := security.MergeFindings(deterministicSecurity, input.Security.Findings)
	repairReasons := make([]string, 0)
	humanReasons := make([]string, 0)

	if input.Test.ExitCode != 0 || !input.Test.Passed {
		repairReasons = append(repairReasons, fmt.Sprintf("test command failed with exit code %d", input.Test.ExitCode))
	}
	for _, finding := range input.Review.Findings {
		if finding.Severity != domain.SeverityBlocking {
			continue
		}
		decision.FindingIDs = append(decision.FindingIDs, finding.ID)
		if finding.Confirmed {
			repairReasons = append(repairReasons, "confirmed blocking review finding: "+finding.ID)
		} else {
			humanReasons = append(humanReasons, "unconfirmed blocking review finding: "+finding.ID)
		}
	}
	for _, finding := range securityFindings {
		if finding.Severity != domain.SeverityHigh && finding.Severity != domain.SeverityCritical {
			continue
		}
		decision.FindingIDs = append(decision.FindingIDs, finding.ID)
		if finding.Confirmed {
			repairReasons = append(repairReasons, "confirmed high security finding: "+finding.ID)
		} else {
			humanReasons = append(humanReasons, "uncertain high security finding: "+finding.ID)
		}
	}

	if reasons := policyViolations(*input.Diff, *input.Plan, input.Budget); len(reasons) > 0 {
		decision.Action = domain.JudgeFail
		decision.Reasons = append(decision.Reasons, reasons...)
		return normalize(decision)
	}
	if allowed, reason := input.Budget.ModelUsageAllowed(); !allowed {
		decision.Action = domain.JudgeFail
		decision.Reasons = append(decision.Reasons, reason)
		return normalize(decision)
	}
	if input.Budget.MaxModelCalls > 0 && input.Budget.ModelCalls > input.Budget.MaxModelCalls {
		decision.Action = domain.JudgeFail
		decision.Reasons = append(decision.Reasons, "model call budget exceeded")
		return normalize(decision)
	}
	if input.Budget.MaxToolCalls > 0 && input.Budget.ToolCalls > input.Budget.MaxToolCalls {
		decision.Action = domain.JudgeFail
		decision.Reasons = append(decision.Reasons, "tool call budget exceeded")
		return normalize(decision)
	}
	if input.Budget.MaxToolOutputBytes > 0 && input.Budget.ToolOutputBytes > input.Budget.MaxToolOutputBytes {
		decision.Action = domain.JudgeFail
		decision.Reasons = append(decision.Reasons, "tool output byte budget exceeded")
		return normalize(decision)
	}

	if len(repairReasons) > 0 {
		decision.Reasons = append(decision.Reasons, repairReasons...)
		if repairAvailable(input) {
			decision.Action = domain.JudgeRepair
		} else {
			decision.Action = domain.JudgeFail
			decision.Reasons = append(decision.Reasons, "repair budget is exhausted")
		}
		return normalize(decision)
	}
	if len(humanReasons) > 0 {
		decision.Action = domain.JudgeHumanReview
		decision.Reasons = append(decision.Reasons, humanReasons...)
		return normalize(decision)
	}
	decision.Reasons = []string{"all deterministic gates passed"}
	return normalize(decision)
}

func repairAvailable(input Input) bool {
	if input.Budget.MaxRepairs <= 0 || input.RepairCount >= input.Budget.MaxRepairs {
		return false
	}
	return input.Budget.MaxIterations <= 0 || input.Iteration+1 < input.Budget.MaxIterations
}

func policyViolations(diff domain.DiffArtifact, plan domain.ExecutionPlan, budget domain.RunBudget) []string {
	reasons := make([]string, 0)
	if len(diff.ChangedFiles) == 0 {
		reasons = append(reasons, "implementation produced an empty diff")
	}
	approved := append([]string(nil), plan.FilesLikelyAffected...)
	slices.Sort(approved)
	for _, file := range diff.ChangedFiles {
		if forbiddenFile(file) {
			reasons = append(reasons, "diff modifies a protected file: "+file)
		}
		if _, exists := slices.BinarySearch(approved, file); !exists {
			reasons = append(reasons, "diff contains a file outside the approved plan: "+file)
		}
	}
	if budget.MaxChangedFiles > 0 && len(diff.ChangedFiles) > budget.MaxChangedFiles {
		reasons = append(reasons, "changed file budget exceeded")
	}
	if budget.MaxDiffBytes > 0 && int64(len(diff.Patch)) > budget.MaxDiffBytes {
		reasons = append(reasons, "diff byte budget exceeded")
	}
	changedLines := 0
	for _, line := range strings.Split(diff.Patch, "\n") {
		if (strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")) || (strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")) {
			changedLines++
		}
	}
	if budget.MaxDiffLines > 0 && changedLines > budget.MaxDiffLines {
		reasons = append(reasons, "diff line budget exceeded")
	}
	return reasons
}

func forbiddenFile(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	base := filepath.Base(normalized)
	if normalized == ".git" || strings.HasPrefix(normalized, ".git/") || strings.Contains(normalized, "/.git/") {
		return true
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == "id_rsa" || base == "id_ed25519" {
		return true
	}
	extension := strings.ToLower(filepath.Ext(base))
	return extension == ".pem" || extension == ".key" || extension == ".p12" || extension == ".pfx"
}

func inputDigest(input Input) string {
	budget := input.Budget
	// Resuming the interrupted judge node increments runtime bookkeeping before
	// approval validation. NodeCalls is not a judge gate.
	budget.NodeCalls = 0
	payload, err := json.Marshal(digestInput{
		Test: input.Test, Review: input.Review, Security: input.Security,
		AssessmentErrors: input.AssessmentErrors, Diff: input.Diff, Plan: input.Plan,
		Budget: budget, RepairCount: input.RepairCount, Iteration: input.Iteration,
		WorkspaceID: input.WorkspaceID,
	})
	if err != nil {
		payload = []byte("judge-input-encoding-failed")
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func normalize(decision domain.JudgeDecision) domain.JudgeDecision {
	sort.Strings(decision.Reasons)
	sort.Strings(decision.FindingIDs)
	decision.Reasons = slices.Compact(decision.Reasons)
	decision.FindingIDs = slices.Compact(decision.FindingIDs)
	return decision
}
