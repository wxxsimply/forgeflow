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
		DeveloperPromptVersion: "developer/v3",
		ReviewerModel:          "model-v1",
		ReviewerPromptVersion:  "reviewer/v1",
		SecurityModel:          "model-v1",
		SecurityPromptVersion:  "security/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := catalog.Prompt("developer", "developer/v3")
	if err != nil {
		t.Fatal(err)
	}
	previousV2, err := catalog.Prompt("developer", "developer/v2")
	if err != nil {
		t.Fatal(err)
	}
	previousV1, err := catalog.Prompt("developer", "developer/v1")
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Configured || previousV2.Configured || previousV1.Configured {
		t.Fatalf("configured flags candidate=%t previousV2=%t previousV1=%t", candidate.Configured, previousV2.Configured, previousV1.Configured)
	}
	if candidate.SHA256 == "" || previousV2.SHA256 == "" || previousV1.SHA256 == "" || candidate.SHA256 == previousV2.SHA256 || candidate.SHA256 == previousV1.SHA256 || previousV2.SHA256 == previousV1.SHA256 {
		t.Fatalf("prompt digests candidate=%q previousV2=%q previousV1=%q", candidate.SHA256, previousV2.SHA256, previousV1.SHA256)
	}
}
