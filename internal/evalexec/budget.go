package evalexec

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

var ErrCostBudgetExceeded = errors.New("eval cost budget exceeded")

// CostBudget coordinates one hard USD ceiling across every baseline and model
// call in an eval campaign. Calls reserve their conservative maximum cost before
// contacting the provider, then replace that reservation with measured cost.
type CostBudget struct {
	mu       sync.Mutex
	limitUSD float64
	spentUSD float64
	reserved float64
}

type costReservation struct {
	budget *CostBudget
	maxUSD float64
	done   bool
}

func NewCostBudget(limitUSD, alreadySpentUSD float64) (*CostBudget, error) {
	if !finitePositive(limitUSD) {
		return nil, fmt.Errorf("eval cost budget must be a finite positive USD amount")
	}
	if math.IsNaN(alreadySpentUSD) || math.IsInf(alreadySpentUSD, 0) || alreadySpentUSD < 0 {
		return nil, fmt.Errorf("eval cost already spent must be a finite non-negative USD amount")
	}
	if alreadySpentUSD > limitUSD {
		return nil, fmt.Errorf("%w: already spent $%.9f exceeds limit $%.9f", ErrCostBudgetExceeded, alreadySpentUSD, limitUSD)
	}
	return &CostBudget{limitUSD: limitUSD, spentUSD: alreadySpentUSD}, nil
}

func (b *CostBudget) reserve(maxUSD float64) (*costReservation, error) {
	if b == nil {
		return nil, nil
	}
	if !finitePositive(maxUSD) {
		return nil, fmt.Errorf("model call maximum cost must be a finite positive USD amount")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limitUSD - b.spentUSD - b.reserved
	if maxUSD > remaining+1e-12 {
		return nil, fmt.Errorf("%w: call requires up to $%.9f but only $%.9f remains", ErrCostBudgetExceeded, maxUSD, max(0, remaining))
	}
	b.reserved += maxUSD
	return &costReservation{budget: b, maxUSD: maxUSD}, nil
}

func (r *costReservation) commit(actualUSD float64) error {
	if r == nil || r.budget == nil || r.done {
		return nil
	}
	if math.IsNaN(actualUSD) || math.IsInf(actualUSD, 0) || actualUSD < 0 {
		return fmt.Errorf("model call cost must be a finite non-negative USD amount")
	}
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	r.done = true
	b.reserved -= r.maxUSD
	b.spentUSD += actualUSD
	if b.spentUSD > b.limitUSD+1e-12 {
		return fmt.Errorf("%w: provider-reported cost raised total to $%.9f above limit $%.9f", ErrCostBudgetExceeded, b.spentUSD, b.limitUSD)
	}
	return nil
}

func (r *costReservation) cancel() {
	if r == nil || r.budget == nil || r.done {
		return
	}
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	r.done = true
	b.reserved -= r.maxUSD
}

func (b *CostBudget) Snapshot() (spentUSD, remainingUSD float64) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spentUSD, max(0, b.limitUSD-b.spentUSD-b.reserved)
}

func finitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}
