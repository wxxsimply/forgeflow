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
}
