package migrations

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrationsArePairedAndContainControlPlaneTables(t *testing.T) {
	all, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 6 || all[0].Version != 1 || all[1].Version != 2 || all[2].Version != 3 || all[3].Version != 4 || all[4].Version != 5 || all[5].Version != 6 || strings.TrimSpace(all[5].Down) == "" {
		t.Fatalf("migrations=%+v", all)
	}
	if !strings.Contains(all[4].Up, "ADD COLUMN model") {
		t.Fatal("prompt release model migration is missing")
	}
	for _, table := range []string{"user_data_exports", "user_deletion_requests"} {
		if !strings.Contains(all[5].Up, "CREATE TABLE "+table) {
			t.Fatalf("user data lifecycle migration does not create %s", table)
		}
	}
	if !strings.Contains(all[5].Up, "ON DELETE CASCADE") || !strings.Contains(all[5].Up, "deletion_pending") {
		t.Fatal("user data lifecycle migration does not enforce deletion ownership")
	}
	for _, table := range []string{"eval_runs", "prompt_releases"} {
		if !strings.Contains(all[3].Up, "CREATE TABLE "+table) {
			t.Fatalf("governance migration does not create %s", table)
		}
	}
	for _, table := range []string{"runs", "run_events", "checkpoints", "outbox", "jobs", "artifacts", "model_calls", "tool_calls"} {
		if !strings.Contains(all[0].Up, "CREATE TABLE "+table) {
			t.Fatalf("initial migration does not create %s", table)
		}
	}
	for _, table := range []string{"users", "sessions", "repositories", "idempotency_keys", "audit_log"} {
		if !strings.Contains(all[1].Up, "CREATE TABLE "+table) {
			t.Fatalf("HTTP API migration does not create %s", table)
		}
	}
}
