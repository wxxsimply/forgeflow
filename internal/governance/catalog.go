package governance

import (
	"fmt"
	"sort"

	"forgeflow/internal/config"
	"forgeflow/internal/developer"
	"forgeflow/internal/planner"
	"forgeflow/internal/reviewer"
	"forgeflow/internal/security"
)

type Agent struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Model         string `json:"model"`
	PromptVersion string `json:"promptVersion"`
	Role          string `json:"role"`
}

type Prompt struct {
	Agent      string `json:"agent"`
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	Configured bool   `json:"configured"`
}

type Catalog struct {
	agents  []Agent
	prompts map[string]Prompt
}

func NewCatalog(configuration config.Config) (*Catalog, error) {
	prompts := map[string]Prompt{}
	add := func(agent, version, sha string) {
		prompts[agent+"|"+version] = Prompt{Agent: agent, Version: version, SHA256: sha, Configured: true}
	}
	plannerPrompt, err := planner.NewPromptLoader(nil).Load(configuration.PlannerPromptVersion)
	if err != nil {
		return nil, err
	}
	developerPrompt, err := developer.NewPromptLoader(nil).Load(configuration.DeveloperPromptVersion)
	if err != nil {
		return nil, err
	}
	reviewerPrompt, err := reviewer.NewPromptLoader(nil).Load(configuration.ReviewerPromptVersion)
	if err != nil {
		return nil, err
	}
	securityPrompt, err := security.NewPromptLoader(nil).Load(configuration.SecurityPromptVersion)
	if err != nil {
		return nil, err
	}
	add("planner", plannerPrompt.Version, plannerPrompt.SHA256)
	add("developer", developerPrompt.Version, developerPrompt.SHA256)
	add("reviewer", reviewerPrompt.Version, reviewerPrompt.SHA256)
	add("security", securityPrompt.Version, securityPrompt.SHA256)
	return &Catalog{agents: []Agent{
		{Name: "planner", Version: "v1", Model: configuration.PlannerModel, PromptVersion: plannerPrompt.Version, Role: "bounded planning"},
		{Name: "developer", Version: "v1", Model: configuration.DeveloperModel, PromptVersion: developerPrompt.Version, Role: "approved implementation"},
		{Name: "reviewer", Version: "v1", Model: configuration.ReviewerModel, PromptVersion: reviewerPrompt.Version, Role: "independent review"},
		{Name: "security", Version: "v1", Model: configuration.SecurityModel, PromptVersion: securityPrompt.Version, Role: "independent security review"},
	}, prompts: prompts}, nil
}

func (c *Catalog) Agents() []Agent {
	return append([]Agent(nil), c.agents...)
}

func (c *Catalog) Prompts() []Prompt {
	out := make([]Prompt, 0, len(c.prompts))
	for _, prompt := range c.prompts {
		out = append(out, prompt)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Agent == out[j].Agent {
			return out[i].Version < out[j].Version
		}
		return out[i].Agent < out[j].Agent
	})
	return out
}

func (c *Catalog) Prompt(agent, version string) (Prompt, error) {
	prompt, ok := c.prompts[agent+"|"+version]
	if !ok {
		return Prompt{}, fmt.Errorf("prompt version is not embedded in this release")
	}
	return prompt, nil
}
