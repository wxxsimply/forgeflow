package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forgeflow/internal/domain"
	"forgeflow/internal/observability"
)

func TestWorkerStatusHandlerExposesHealthAndMetrics(t *testing.T) {
	telemetry, err := observability.NewTelemetry(context.Background(), observability.Options{ServiceName: "worker-test", Environment: "test", SampleRatio: 0, Metrics: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = telemetry.Shutdown(context.Background()) })

	for path, expected := range map[string]string{"/healthz": "\"status\":\"ok\"", "/readyz": "\"status\":\"ready\"", "/metrics": "text/plain"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		workerStatusHandler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, response.Code)
		}
		if path != "/metrics" && !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("health body=%s", response.Body.String())
		}
		if path == "/metrics" && !strings.Contains(response.Header().Get("Content-Type"), expected) {
			t.Fatalf("metrics content type=%s", response.Header().Get("Content-Type"))
		}
	}
}

func TestWorkerReadinessFailsClosedOnReleaseMismatch(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	workerStatusHandler(func(context.Context) error { return context.Canceled }).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "active_release_mismatch") {
		t.Fatalf("readiness status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWorkerOnlySkipsUnresolvedInterruptions(t *testing.T) {
	pending := &domain.RunState{Status: domain.StatusWaitingPlanApproval, PendingApproval: &domain.ApprovalRequest{Status: domain.ApprovalPending}}
	if workerShouldExecute(pending) {
		t.Fatal("worker executed an unresolved approval")
	}
	approved := *pending
	approved.PendingApproval = &domain.ApprovalRequest{Status: domain.ApprovalApproved}
	if !workerShouldExecute(&approved) {
		t.Fatal("worker did not resume an approved run")
	}
	cancellation := *pending
	cancellation.RequestCancellation("user", "stop")
	if !workerShouldExecute(&cancellation) {
		t.Fatal("worker did not propagate cancellation from an interrupted run")
	}
	if workerShouldExecute(&domain.RunState{Status: domain.StatusCompleted}) {
		t.Fatal("worker executed a terminal run")
	}
}
