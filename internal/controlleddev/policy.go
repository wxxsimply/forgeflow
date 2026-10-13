package controlleddev

import (
	"fmt"
	"net/mail"
	"path/filepath"

	"forgeflow/internal/auth"
	"forgeflow/internal/domain"
)

// Policy limits the experimental development workflow to one administrator and
// one explicitly mounted fixture repository. It is not a substitute for an
// isolated execution host or a provider-side spending limit.
type Policy struct {
	AdminEmail     string
	RepositoryPath string
}

func (p Policy) Validate() error {
	address, err := mail.ParseAddress(p.AdminEmail)
	if err != nil || address.Address != p.AdminEmail || auth.NormalizeEmail(p.AdminEmail) != p.AdminEmail {
		return fmt.Errorf("controlled development requires an administrator email")
	}
	if !filepath.IsAbs(p.RepositoryPath) ||
		filepath.Clean(p.RepositoryPath) != p.RepositoryPath ||
		filepath.Dir(p.RepositoryPath) == p.RepositoryPath {
		return fmt.Errorf("controlled development requires one canonical absolute repository path")
	}
	return nil
}

func (p Policy) Allows(user auth.User, repositoryPath string) bool {
	return user.Role == auth.RoleAdmin && user.Status == "active" &&
		auth.NormalizeEmail(user.Email) == auth.NormalizeEmail(p.AdminEmail) &&
		repositoryPath == p.RepositoryPath
}

func (p Policy) Budget() domain.RunBudget {
	budget := domain.DefaultRunBudget(1)
	budget.MaxModelCalls = 4 // planner, developer, reviewer, security; no repair call
	budget.MaxRepairs = 0
	budget.MaxEstimatedCostUSD = 0.5 // secondary estimate, not a billing hard cap
	return budget
}

func (p Policy) AllowsState(user auth.User, state *domain.RunState) bool {
	if state == nil || state.OwnerID != user.ID || !p.Allows(user, state.RepositoryPath) {
		return false
	}
	actual := state.Budget
	if state.MaxIterations != 1 || actual.NodeCalls < 0 || actual.ModelCalls < 0 || actual.ModelCalls > 4 ||
		actual.ToolCalls < 0 || actual.ToolOutputBytes < 0 || actual.InputTokens < 0 || actual.OutputTokens < 0 || actual.EstimatedCostUSD < 0 {
		return false
	}
	actual.NodeCalls, actual.ModelCalls, actual.ToolCalls = 0, 0, 0
	actual.ToolOutputBytes, actual.InputTokens, actual.OutputTokens, actual.EstimatedCostUSD = 0, 0, 0, 0
	return actual == p.Budget()
}
