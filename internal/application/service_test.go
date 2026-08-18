package application

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
	"forgeflow/internal/planner"
)

func TestPlanningFlowWaitsAndResumes(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	service := NewService(checkpoint.NewFileStore(directory), planner.Mock{})
	waiting, err := service.Create(context.Background(), CreateInput{Task: "add idempotency", RepositoryPath: "."})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if waiting.Status != domain.StatusWaitingPlanApproval {
		t.Fatalf("status = %q, want %q", waiting.Status, domain.StatusWaitingPlanApproval)
	}
	if waiting.PendingApproval == nil || waiting.PendingApproval.Status != domain.ApprovalPending {
		t.Fatal("expected pending plan approval")
	}
	// Use a new service and store instance to simulate process restart from the durable checkpoint.
	resumedService := NewService(checkpoint.NewFileStore(directory), planner.Mock{})
	completed, err := resumedService.ResolveApproval(context.Background(), waiting.RunID, true, "looks good")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if completed.Status != domain.StatusCompleted {
		t.Fatalf("status = %q, want %q", completed.Status, domain.StatusCompleted)
	}
}

func TestOpenAIPlannerDoesNotPersistAPIKeyOrFullPrompt(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{
          "id":"resp-secret-test","model":"fixture","status":"completed",
          "output":[{"type":"message","content":[{"type":"output_text","text":"{\"summary\":\"safe plan\",\"assumptions\":[],\"filesLikelyAffected\":[],\"steps\":[{\"id\":\"one\",\"description\":\"Inspect\",\"acceptanceCriteria\":[\"Done\"],\"dependsOn\":[]}],\"risks\":[],\"testStrategy\":[\"Test\"]}"}]}],
          "usage":{"input_tokens":10,"output_tokens":10,"total_tokens":20}
        }`))
	}))
	defer server.Close()
	const secret = "checkpoint-must-not-contain-this-key"
	provider, err := model.NewOpenAIProvider(model.OpenAIConfig{APIKey: secret, BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := planner.NewAgent(planner.Options{Provider: provider, Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	service := NewService(checkpoint.NewFileStore(directory), agent)
	if _, err := service.Create(context.Background(), CreateInput{Task: "safe fixture task", RepositoryPath: "."}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("Produce a bounded software implementation plan")) {
			t.Fatalf("checkpoint %s persisted a secret or full system prompt", entry.Name())
		}
	}
}

func TestProviderPlannerUsagePersistsBeforeApproval(t *testing.T) {
	t.Parallel()
	fake := &model.FakeProvider{Responses: []model.Response{{
		ID: "resp-app", Model: "gpt-fixture", Status: "completed",
		OutputText: `{
          "summary":"provider plan","assumptions":[],"filesLikelyAffected":[],
          "steps":[{"id":"inspect","description":"Inspect","acceptanceCriteria":["Inspected"],"dependsOn":[]}],
          "risks":[{"level":"low","description":"Read-only planning"}],"testStrategy":["Run tests"]
        }`,
		Usage: model.Usage{InputTokens: 100, OutputTokens: 50},
	}}}
	agent, err := planner.NewAgent(planner.Options{Provider: fake, Model: "gpt-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(checkpoint.NewFileStore(t.TempDir()), agent)
	state, err := service.Create(context.Background(), CreateInput{Task: "fixture", RepositoryPath: "."})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if state.Status != domain.StatusWaitingPlanApproval || len(state.ModelInvocations) != 1 || state.Budget.ModelCalls != 1 {
		t.Fatalf("state = %+v", state)
	}
	if state.ModelInvocations[0].ResponseID != "resp-app" || state.Budget.InputTokens != 100 || state.Budget.OutputTokens != 50 {
		t.Fatalf("invocation = %+v budget = %+v", state.ModelInvocations[0], state.Budget)
	}
}

func TestPlanningFlowRejects(t *testing.T) {
	t.Parallel()
	service := NewService(checkpoint.NewFileStore(t.TempDir()), planner.Mock{})
	waiting, err := service.Create(context.Background(), CreateInput{Task: "test", RepositoryPath: "."})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cancelled, err := service.ResolveApproval(context.Background(), waiting.RunID, false, "too risky")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("status = %q, want %q", cancelled.Status, domain.StatusCancelled)
	}
}

func TestCancelPersistsAndStopsRun(t *testing.T) {
	t.Parallel()
	service := NewService(checkpoint.NewFileStore(t.TempDir()), planner.Mock{})
	waiting, err := service.Create(context.Background(), CreateInput{Task: "test", RepositoryPath: "."})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	cancelled, err := service.Cancel(context.Background(), waiting.RunID, "test-user", "no longer needed")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("status = %q, want %q", cancelled.Status, domain.StatusCancelled)
	}
	if !cancelled.Cancellation.Requested() || cancelled.Cancellation.RequestedBy != "test-user" {
		t.Fatal("cancellation request was not persisted")
	}
}
