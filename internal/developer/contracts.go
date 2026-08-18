package developer

import (
	"context"
	"sync"

	"forgeflow/internal/domain"
)

type Input struct {
	RunID            string
	NodeID           string
	Task             string
	Plan             domain.ExecutionPlan
	Workspace        domain.WorkspaceRef
	Budget           domain.RunBudget
	ToolNames        []string
	PreviousTest     *domain.TestAssessment
	CurrentDiff      *domain.DiffArtifact
	ReviewFindings   []domain.ReviewFinding
	SecurityFindings []domain.SecurityFinding
}

type Result struct {
	Implementation domain.ImplementationResult
	Invocation     *domain.ModelInvocation
}

type Developer interface {
	Name() string
	Version() string
	Execute(context.Context, Input) (Result, error)
}

type Fake struct {
	mu      sync.Mutex
	Results []Result
	Errors  []error
	Inputs  []Input
	calls   int
}

func (*Fake) Name() string    { return "developer" }
func (*Fake) Version() string { return "fake/v1" }

func (f *Fake) Execute(_ context.Context, input Input) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Inputs = append(f.Inputs, input)
	index := f.calls
	f.calls++
	if index < len(f.Errors) && f.Errors[index] != nil {
		return Result{}, f.Errors[index]
	}
	if index >= len(f.Results) {
		return Result{}, nil
	}
	return f.Results[index], nil
}

func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

var _ Developer = (*Fake)(nil)
