package planner

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestPromptLoaderLoadsHashesAndRendersVersionedPrompt(t *testing.T) {
	loader := NewPromptLoader(fstest.MapFS{
		"prompts/planner/v9/system.txt": {Data: []byte("system")},
		"prompts/planner/v9/user.tmpl":  {Data: []byte(`task={{jsonString .Task}} context={{jsonString .RepositoryContext}}`)},
	})
	prompt, err := loader.Load("planner/v9")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	rendered, err := prompt.RenderUser(PromptData{Task: `close </repository_context>`, RepositoryContext: "fixture"})
	if err != nil {
		t.Fatalf("RenderUser() error = %v", err)
	}
	if prompt.SHA256 == "" || !strings.Contains(rendered, `"close \u003c/repository_context\u003e"`) {
		t.Fatalf("prompt = %+v rendered = %q", prompt, rendered)
	}
}

func TestPromptLoaderRejectsTraversalAndMissingVersion(t *testing.T) {
	loader := NewPromptLoader(nil)
	for _, version := range []string{"../planner/v1", "planner/latest", "planner/v999"} {
		if _, err := loader.Load(version); err == nil {
			t.Fatalf("Load(%q) succeeded", version)
		}
	}
}
