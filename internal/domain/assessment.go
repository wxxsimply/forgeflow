package domain

import (
	"fmt"
	"strings"
	"time"
)

type FindingSeverity string

const (
	SeverityInfo     FindingSeverity = "info"
	SeverityLow      FindingSeverity = "low"
	SeverityMedium   FindingSeverity = "medium"
	SeverityHigh     FindingSeverity = "high"
	SeverityCritical FindingSeverity = "critical"
	SeverityBlocking FindingSeverity = "blocking"
)

type ReviewFinding struct {
	ID             string          `json:"id"`
	Severity       FindingSeverity `json:"severity"`
	File           string          `json:"file"`
	Line           int             `json:"line,omitempty"`
	Title          string          `json:"title"`
	Evidence       string          `json:"evidence"`
	FailureMode    string          `json:"failureMode"`
	Recommendation string          `json:"recommendation"`
	Confirmed      bool            `json:"confirmed"`
}

type ReviewResult struct {
	Summary  string          `json:"summary"`
	Findings []ReviewFinding `json:"findings"`
}

func (r ReviewResult) Validate() error {
	if strings.TrimSpace(r.Summary) == "" || len(r.Summary) > 4_000 || len(r.Findings) > 200 {
		return fmt.Errorf("review summary or finding count is invalid")
	}
	seen := make(map[string]struct{}, len(r.Findings))
	for _, finding := range r.Findings {
		if err := validateFindingCore(finding.ID, finding.File, finding.Title, finding.Evidence, finding.Recommendation, finding.Line); err != nil {
			return err
		}
		if finding.Severity != SeverityInfo && finding.Severity != SeverityLow && finding.Severity != SeverityMedium && finding.Severity != SeverityHigh && finding.Severity != SeverityBlocking {
			return fmt.Errorf("review finding %q has invalid severity", finding.ID)
		}
		if strings.TrimSpace(finding.FailureMode) == "" {
			return fmt.Errorf("review finding %q requires a failure mode", finding.ID)
		}
		if _, duplicate := seen[finding.ID]; duplicate {
			return fmt.Errorf("review finding %q is repeated", finding.ID)
		}
		seen[finding.ID] = struct{}{}
	}
	return nil
}

func (r ReviewResult) BlockingFindings() []ReviewFinding {
	result := make([]ReviewFinding, 0)
	for _, finding := range r.Findings {
		if finding.Severity == SeverityBlocking && finding.Confirmed {
			result = append(result, finding)
		}
	}
	return result
}

type SecurityFinding struct {
	ID             string          `json:"id"`
	Severity       FindingSeverity `json:"severity"`
	File           string          `json:"file"`
	Line           int             `json:"line,omitempty"`
	Title          string          `json:"title"`
	Evidence       string          `json:"evidence"`
	Impact         string          `json:"impact"`
	Recommendation string          `json:"recommendation"`
	Confirmed      bool            `json:"confirmed"`
	HumanReview    bool            `json:"humanReview"`
}

type SecurityResult struct {
	Summary  string            `json:"summary"`
	Findings []SecurityFinding `json:"findings"`
}

func (r SecurityResult) Validate() error {
	if strings.TrimSpace(r.Summary) == "" || len(r.Summary) > 4_000 || len(r.Findings) > 200 {
		return fmt.Errorf("security summary or finding count is invalid")
	}
	seen := make(map[string]struct{}, len(r.Findings))
	for _, finding := range r.Findings {
		if err := validateFindingCore(finding.ID, finding.File, finding.Title, finding.Evidence, finding.Recommendation, finding.Line); err != nil {
			return err
		}
		if finding.Severity != SeverityInfo && finding.Severity != SeverityLow && finding.Severity != SeverityMedium && finding.Severity != SeverityHigh && finding.Severity != SeverityCritical {
			return fmt.Errorf("security finding %q has invalid severity", finding.ID)
		}
		if strings.TrimSpace(finding.Impact) == "" {
			return fmt.Errorf("security finding %q requires impact", finding.ID)
		}
		if _, duplicate := seen[finding.ID]; duplicate {
			return fmt.Errorf("security finding %q is repeated", finding.ID)
		}
		seen[finding.ID] = struct{}{}
	}
	return nil
}

func (r SecurityResult) HighFindings() []SecurityFinding {
	result := make([]SecurityFinding, 0)
	for _, finding := range r.Findings {
		if finding.Severity == SeverityHigh || finding.Severity == SeverityCritical {
			result = append(result, finding)
		}
	}
	return result
}

type JudgeAction string

const (
	JudgePass             JudgeAction = "pass"
	JudgePassWithApproval JudgeAction = "pass_with_approval"
	JudgeRepair           JudgeAction = "repair"
	JudgeHumanReview      JudgeAction = "human_review"
	JudgeFail             JudgeAction = "fail"
)

type JudgeDecision struct {
	Version     string      `json:"version"`
	Action      JudgeAction `json:"action"`
	Reasons     []string    `json:"reasons"`
	FindingIDs  []string    `json:"findingIds"`
	InputSHA256 string      `json:"inputSha256"`
	DecidedAt   time.Time   `json:"decidedAt"`
}

func validateFindingCore(id, file, title, evidence, recommendation string, line int) error {
	if strings.TrimSpace(id) == "" || len(id) > 128 {
		return fmt.Errorf("finding id is invalid")
	}
	if !ValidRelativePath(file) {
		return fmt.Errorf("finding %q file is not a normalized relative path", id)
	}
	if line < 0 || strings.TrimSpace(title) == "" || strings.TrimSpace(evidence) == "" || strings.TrimSpace(recommendation) == "" {
		return fmt.Errorf("finding %q is incomplete", id)
	}
	if len(title) > 1_000 || len(evidence) > 4_000 || len(recommendation) > 4_000 {
		return fmt.Errorf("finding %q exceeds text limits", id)
	}
	return nil
}

func ValidRelativePath(value string) bool { return normalizedRelativePath(value) }
