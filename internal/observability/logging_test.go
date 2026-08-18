package observability

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerWritesStructuredContext(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger, err := NewLogger(&output, "info", "test")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	logger.Info("run created", "run_id", "run-1")

	line := output.String()
	for _, expected := range []string{`"service":"forgeflow-cli"`, `"environment":"test"`, `"run_id":"run-1"`} {
		if !strings.Contains(line, expected) {
			t.Fatalf("log output %q does not contain %q", line, expected)
		}
	}
}

func TestLoggerRejectsUnknownLevel(t *testing.T) {
	t.Parallel()
	if _, err := NewLogger(&bytes.Buffer{}, "verbose", "test"); err == nil {
		t.Fatal("NewLogger() accepted an unknown level")
	}
}
