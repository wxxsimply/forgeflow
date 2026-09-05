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
		DeveloperPromptVersion: "developer/v4",
		ReviewerModel:          "model-v1",
		ReviewerPromptVersion:  "reviewer/v1",
		SecurityModel:          "model-v1",
		SecurityPromptVersion:  "security/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := catalog.Prompt("developer", "developer/v4")
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
	previousV3, err := catalog.Prompt("developer", "developer/v3")
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Configured || previousV3.Configured || previousV2.Configured || previousV1.Configured {
		t.Fatalf("configured flags candidate=%t previousV3=%t previousV2=%t previousV1=%t", candidate.Configured, previousV3.Configured, previousV2.Configured, previousV1.Configured)
	}
	digests := map[string]string{"candidate": candidate.SHA256, "previousV3": previousV3.SHA256, "previousV2": previousV2.SHA256, "previousV1": previousV1.SHA256}
	seen := map[string]struct{}{}
	for label, digest := range digests {
		if digest == "" {
			t.Fatalf("%s digest is empty", label)
		}
		if _, duplicate := seen[digest]; duplicate {
			t.Fatalf("prompt digests are not distinct: %v", digests)
		}
		seen[digest] = struct{}{}
	}
}
