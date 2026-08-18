package application

import (
	"context"
	"testing"

	"forgeflow/internal/checkpoint"
	"forgeflow/internal/domain"
	"forgeflow/internal/planner"
)

func TestRunCanPauseAndResumeAtApprovalCheckpoint(t *testing.T) {
	directory := t.TempDir()
	service := NewService(checkpoint.NewFileStore(directory), planner.Mock{})
	waiting, err := service.Create(context.Background(), CreateInput{Task: "pause test", RepositoryPath: "."})
	if err != nil {
		t.Fatal(err)
	}
	approvalID := waiting.PendingApproval.ApprovalID
	paused, err := service.Pause(context.Background(), waiting.RunID, "tester", "maintenance")
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != domain.StatusPaused || paused.ResumeGuard == nil || paused.Pause.PausedAt == nil {
		t.Fatalf("paused=%+v", paused)
	}
	restarted := NewService(checkpoint.NewFileStore(directory), planner.Mock{})
	resumed, err := restarted.Resume(context.Background(), paused.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != domain.StatusWaitingPlanApproval || resumed.PendingApproval == nil || resumed.PendingApproval.ApprovalID != approvalID {
		t.Fatalf("resumed status=%s approval=%+v", resumed.Status, resumed.PendingApproval)
	}
}

func TestResumeRejectsChangedApprovalEvidence(t *testing.T) {
	directory := t.TempDir()
	store := checkpoint.NewFileStore(directory)
	service := NewService(store, planner.Mock{})
	waiting, err := service.Create(context.Background(), CreateInput{Task: "pause test", RepositoryPath: "."})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.Pause(context.Background(), waiting.RunID, "tester", "maintenance")
	if err != nil {
		t.Fatal(err)
	}
	paused.PendingApproval.ApprovalID = domain.NewID()
	if err := store.Save(context.Background(), paused, paused.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resume(context.Background(), paused.RunID); err == nil {
		t.Fatal("resume accepted changed approval evidence")
	}
}

func TestPausedRunCanBeCancelled(t *testing.T) {
	service := NewService(checkpoint.NewFileStore(t.TempDir()), planner.Mock{})
	waiting, err := service.Create(context.Background(), CreateInput{Task: "cancel paused", RepositoryPath: "."})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.Pause(context.Background(), waiting.RunID, "tester", "pause")
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.Cancel(context.Background(), paused.RunID, "tester", "stop")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("status=%s", cancelled.Status)
	}
}
