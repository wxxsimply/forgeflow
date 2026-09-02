package governance

import (
	"testing"

	"forgeflow/internal/config"
)

func TestCatalogResolvesPreviousDeveloperPromptForRollback(t *testing.T) {
	catalog, err := NewCatalog(config.Config{
		PlannerModel:           "model-v1",
		PlannerPromptVersion:   "planner/v1",
		DeveloperModel:         "model-v1",
		DeveloperPromptVersion: "developer/v2",
		ReviewerModel:          "model-v1",
		ReviewerPromptVersion:  "reviewer/v1",
		SecurityModel:          "model-v1",
		SecurityPromptVersion:  "security/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := catalog.Prompt("developer", "developer/v2")
	if err != nil {
		t.Fatal(err)
	}
	previous, err := catalog.Prompt("developer", "developer/v1")
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Configured || previous.Configured {
		t.Fatalf("configured flags candidate=%t previous=%t", candidate.Configured, previous.Configured)
	}
	if candidate.SHA256 == "" || previous.SHA256 == "" || candidate.SHA256 == previous.SHA256 {
		t.Fatalf("prompt digests candidate=%q previous=%q", candidate.SHA256, previous.SHA256)
	}
}
