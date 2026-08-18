package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsExposeBoundedPrometheusFamilies(t *testing.T) {
	metrics := NewMetrics(true)
	metrics.HTTP(http.MethodGet, "/api/v1/runs/{runId}", http.StatusOK, 25*time.Millisecond)
	metrics.Run("completed", true, true, true, 1)
	metrics.Node("planner", "completed", 50*time.Millisecond)
	metrics.Model("openai", "gpt-test", "completed", 10, 4, 2, 0.01, time.Second)
	metrics.Tool("read_file", "succeeded", "allow", 10*time.Millisecond)
	metrics.Approval("approved", time.Minute)
	metrics.Queue("leased")
	metrics.QueueDepth(3)
	metrics.Auth("success")
	metrics.RateLimited("api")
	output := metrics.Prometheus()
	for _, expected := range []string{"forgeflow_http_requests_total", "forgeflow_runs_terminal_total", "forgeflow_runs_first_pass_total", "forgeflow_runs_recovery_total", "forgeflow_budget_exhaustions_total", "forgeflow_repairs_total", "forgeflow_graph_node_duration_seconds", "forgeflow_model_tokens_total", "forgeflow_tool_calls_total", "forgeflow_approval_wait_duration_seconds", "forgeflow_queue_events_total", "forgeflow_queue_depth", "forgeflow_auth_attempts_total", "forgeflow_rate_limit_rejections_total"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("metrics output does not contain %s:\n%s", expected, output)
		}
	}
	for _, forbidden := range []string{"user_id", "task=", "repository_path"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("metrics contain forbidden high-cardinality label %q", forbidden)
		}
	}
	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics response = %d %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
}

func TestDisabledMetricsAndTelemetryDoNotRequireCollector(t *testing.T) {
	metrics := NewMetrics(false)
	metrics.RunTerminal("completed")
	if metrics.Prometheus() != "" {
		t.Fatal("disabled metrics emitted data")
	}
	telemetry, err := NewTelemetry(context.Background(), Options{ServiceName: "test", Environment: "test", SampleRatio: 0.1})
	if err != nil {
		t.Fatalf("NewTelemetry() error = %v", err)
	}
	ctx, span := StartNodeSpan(context.Background(), "run", "trace", "planner", 1)
	if ctx == nil || span == nil {
		t.Fatal("disabled telemetry did not return safe no-op span")
	}
	EndSpan(span, nil, "completed")
	if err := telemetry.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestMetricsSanitizeUnexpectedLabelValues(t *testing.T) {
	metrics := NewMetrics(true)
	metrics.HTTP("UNBOUNDED-METHOD-VALUE", "/runs/secret?id=1", 599, time.Second)
	output := metrics.Prometheus()
	if strings.Contains(output, "secret") || !strings.Contains(output, "route=\"unmatched\"") || !strings.Contains(output, "method=\"OTHER\"") {
		t.Fatalf("labels were not bounded:\n%s", output)
	}
}
