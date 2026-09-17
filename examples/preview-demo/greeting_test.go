package greeting

import "testing"

func TestGreet(t *testing.T) {
	tests := []struct {
		name string
		input string
		want  string
	}{
		{name: "ordinary name", input: "ForgeFlow", want: "Hello, ForgeFlow!"},
		{name: "Chinese name", input: "张三", want: "Hello, 张三!"},
		{name: "surrounding whitespace", input: "  ForgeFlow  ", want: "Hello, ForgeFlow!"},
		{name: "empty name", input: "", want: "Hello, guest!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Greet(tt.input); got != tt.want {
				t.Fatalf("Greet(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
