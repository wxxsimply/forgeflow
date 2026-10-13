package controlleddev

import (
	"path/filepath"
	"testing"

	"forgeflow/internal/auth"
	"forgeflow/internal/domain"
)

func TestPolicyAllowsOnlyNamedActiveAdminAndExactRepository(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "demo")
	policy := Policy{AdminEmail: "admin@example.com", RepositoryPath: repository}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	user := auth.User{ID: "admin-id", Email: "Admin@Example.com", Role: auth.RoleAdmin, Status: "active"}
	if !policy.Allows(user, repository) || policy.Allows(user, repository+"-other") {
		t.Fatal("repository allowlist was not exact")
	}
	user.Role = auth.RoleOperator
	if policy.Allows(user, repository) {
		t.Fatal("operator was allowed")
	}
	user.Role = auth.RoleAdmin
	user.Status = "disabled"
	if policy.Allows(user, repository) {
		t.Fatal("disabled administrator was allowed")
	}
	for _, invalid := range []Policy{
		{AdminEmail: "", RepositoryPath: repository},
		{AdminEmail: "@", RepositoryPath: repository},
		{AdminEmail: "admin@example.com", RepositoryPath: "relative/demo"},
		{AdminEmail: "admin@example.com", RepositoryPath: filepath.Join(repository, "..", "demo") + string(filepath.Separator) + ".."},
	} {
		if invalid.Validate() == nil {
			t.Fatalf("accepted invalid policy: %+v", invalid)
		}
	}
}

func TestPolicyBudgetAndWorkerStateGate(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "demo")
	policy := Policy{AdminEmail: "admin@example.com", RepositoryPath: repository}
	user := auth.User{ID: "admin-id", Email: "admin@example.com", Role: auth.RoleAdmin, Status: "active"}
	budget := policy.Budget()
	if budget.MaxModelCalls != 4 || budget.MaxRepairs != 0 || budget.MaxIterations != 1 {
		t.Fatalf("budget=%+v", budget)
	}
	state := domain.NewRunState(domain.NewRunInput{OwnerID: user.ID, RepositoryPath: repository, MaxIterations: 1, Budget: &budget})
	if !policy.AllowsState(user, state) {
		t.Fatal("bounded admin run was denied")
	}
	state.Budget.MaxModelCalls = 20
	if policy.AllowsState(user, state) {
		t.Fatal("unbounded run was allowed")
	}
	state.Budget = budget
	state.Budget.ModelCalls = 5
	if policy.AllowsState(user, state) {
		t.Fatal("over-budget run was allowed")
	}
	state.Budget = budget
	state.Budget.MaxChangedFiles++
	if policy.AllowsState(user, state) {
		t.Fatal("weakened diff limit was allowed")
	}
}
