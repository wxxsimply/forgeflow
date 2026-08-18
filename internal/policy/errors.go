package policy

import "fmt"

type configError struct{ message string }

func (e configError) Error() string { return e.message }

func policyConfigError(message string) error {
	return configError{message: fmt.Sprintf("invalid policy configuration: %s", message)}
}
