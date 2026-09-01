package governance

import (
	"context"
	"fmt"
)

type ActiveReleaseReader interface {
	ActiveRelease(context.Context, string) (PromptRelease, error)
}

// ValidateActiveReleases proves that the worker image, its configured models,
// and the database governance state describe the same immutable release set.
func ValidateActiveReleases(ctx context.Context, releases ActiveReleaseReader, catalog *Catalog) error {
	if releases == nil || catalog == nil {
		return fmt.Errorf("active release readiness is not configured")
	}
	for _, agent := range catalog.Agents() {
		prompt, err := catalog.Prompt(agent.Name, agent.PromptVersion)
		if err != nil {
			return fmt.Errorf("resolve embedded %s prompt: %w", agent.Name, err)
		}
		active, err := releases.ActiveRelease(ctx, agent.Name)
		if err != nil {
			return fmt.Errorf("load active %s release: %w", agent.Name, err)
		}
		if active.Version != prompt.Version || active.PromptSHA256 != prompt.SHA256 || active.Model != agent.Model {
			return fmt.Errorf(
				"active %s release does not match worker image: database prompt=%s sha=%s model=%s, worker prompt=%s sha=%s model=%s",
				agent.Name, active.Version, active.PromptSHA256, active.Model, prompt.Version, prompt.SHA256, agent.Model,
			)
		}
	}
	return nil
}
