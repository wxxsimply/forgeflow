package observability

import (
	"context"
	"testing"
)

func TestTelemetryOptionsValidation(t *testing.T) {
	if _, err := NewTelemetry(context.Background(), Options{}); err == nil {
		t.Fatal("missing service name was accepted")
	}
	if _, err := NewTelemetry(context.Background(), Options{ServiceName: "api", SampleRatio: 1.1}); err == nil {
		t.Fatal("invalid sample ratio was accepted")
	}
	if _, err := NewTelemetry(context.Background(), Options{ServiceName: "api", Environment: "production", SampleRatio: 1, OTLPEndpoint: "http://collector:4318/v1/traces"}); err == nil {
		t.Fatal("insecure Production OTLP endpoint was accepted")
	}
	if _, err := NewTelemetry(context.Background(), Options{ServiceName: "api", Environment: "production", SampleRatio: 1}); err == nil {
		t.Fatal("missing Production OTLP endpoint was accepted")
	}
	telemetry, err := NewTelemetry(context.Background(), Options{ServiceName: "api", Environment: "production", SampleRatio: 1, OTLPEndpoint: "https://traces.example.com/v1/traces", OTLPHeaders: map[string]string{"Authorization": "Bearer test"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := telemetry.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
