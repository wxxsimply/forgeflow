package greeting

import "strings"

func Greet(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "Hello, guest!"
	}
	return "Hello, " + trimmed + "!"
}
