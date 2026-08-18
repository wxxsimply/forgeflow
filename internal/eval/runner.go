package eval

import (
	"context"
	"fmt"
	"time"
)

// Executor runs one fixed case through one configured delivery pipeline and
// returns measured evidence. Implementations own fixture checkout, hidden test
// isolation, and model/tool usage capture; Runner never infers missing values.
type Executor interface {
	Execute(context.Context, Case, Configuration) (Observation, error)
}

type Runner struct {
	Dataset  Dataset
	Executor Executor
	Now      func() time.Time
}

func (r Runner) Run(ctx context.Context, configuration Configuration) (Evidence, error) {
	if err := ValidateDataset(r.Dataset); err != nil {
		return Evidence{}, err
	}
	if r.Executor == nil {
		return Evidence{}, fmt.Errorf("eval executor is required")
	}
	now := r.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	evidence := Evidence{Dataset: r.Dataset.Name, Configuration: configuration, ObservedAt: now().UTC(), Observations: make([]Observation, 0, len(r.Dataset.Cases))}
	for _, evalCase := range r.Dataset.Cases {
		if err := ctx.Err(); err != nil {
			return Evidence{}, err
		}
		observation, err := r.Executor.Execute(ctx, evalCase, configuration)
		if err != nil {
			return Evidence{}, fmt.Errorf("execute eval case %s: %w", evalCase.ID, err)
		}
		if observation.CaseID == "" {
			observation.CaseID = evalCase.ID
		}
		if observation.CaseID != evalCase.ID {
			return Evidence{}, fmt.Errorf("executor returned case %q for %q", observation.CaseID, evalCase.ID)
		}
		evidence.Observations = append(evidence.Observations, observation)
	}
	if err := ValidateObservations(r.Dataset, evidence.Observations); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}
