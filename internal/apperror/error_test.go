package apperror

import (
	"errors"
	"testing"
)

func TestWrapPreservesCauseAndPublicFields(t *testing.T) {
	t.Parallel()
	cause := errors.New("disk unavailable")
	err := Wrap(cause, CodeTransient, "checkpoint.save", "checkpoint is temporarily unavailable")

	if !errors.Is(err, cause) {
		t.Fatal("wrapped error does not preserve its cause")
	}
	if got := CodeOf(err); got != CodeTransient {
		t.Fatalf("CodeOf() = %q, want %q", got, CodeTransient)
	}
	if got := MessageOf(err); got != "checkpoint is temporarily unavailable" {
		t.Fatalf("MessageOf() = %q", got)
	}
}

func TestUnknownErrorsAreInternal(t *testing.T) {
	t.Parallel()
	if got := CodeOf(errors.New("unknown")); got != CodeInternal {
		t.Fatalf("CodeOf() = %q, want %q", got, CodeInternal)
	}
}
