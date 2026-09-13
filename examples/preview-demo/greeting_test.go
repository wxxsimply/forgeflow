package greeting

import "testing"

func TestGreet(t *testing.T) {
	if got := Greet("ForgeFlow"); got != "Hello, ForgeFlow!" {
		t.Fatalf("unexpected greeting: %q", got)
	}
}
