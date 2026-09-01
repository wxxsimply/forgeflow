package governance

import (
	"context"
	"strings"
	"testing"

	"forgeflow/internal/config"
)

type releaseReader map[string]PromptRelease

func (r releaseReader) ActiveRelease(_ context.Context, agent string) (PromptRelease, error) {
	release, ok := r[agent]
	if !ok {
		return PromptRelease{}, context.Canceled
	}
	return release, nil
}

func TestValidateActiveReleasesMatchesPromptSHAAndModel(t *testing.T) {
	configuration := config.Config{
		PlannerModel: "model-v1", PlannerPromptVersion: "planner/v1",
		DeveloperModel: "model-v1", DeveloperPromptVersion: "developer/v1",
		ReviewerModel: "model-v1", ReviewerPromptVersion: "reviewer/v1",
		SecurityModel: "model-v1", SecurityPromptVersion: "security/v1",
	}
	catalog, err := NewCatalog(configuration)
	if err != nil {
		t.Fatal(err)
	}
	releases := releaseReader{}
	for _, agent := range catalog.Agents() {
		prompt, err := catalog.Prompt(agent.Name, agent.PromptVersion)
		if err != nil {
			t.Fatal(err)
		}
		releases[agent.Name] = PromptRelease{Agent: agent.Name, Version: prompt.Version, PromptSHA256: prompt.SHA256, Model: agent.Model}
	}
	if err := ValidateActiveReleases(context.Background(), releases, catalog); err != nil {
		t.Fatalf("matching releases rejected: %v", err)
	}
	mismatch := releases["developer"]
	mismatch.Model = "model-v2"
	releases["developer"] = mismatch
	if err := ValidateActiveReleases(context.Background(), releases, catalog); err == nil || !strings.Contains(err.Error(), "does not match worker image") {
		t.Fatalf("model mismatch error = %v", err)
	}
}
