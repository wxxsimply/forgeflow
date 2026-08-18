package apperror

import "errors"

type Code string

const (
	CodeValidation     Code = "validation_error"
	CodeNotFound       Code = "not_found"
	CodeConflict       Code = "conflict"
	CodeUnauthorized   Code = "unauthorized"
	CodeForbidden      Code = "forbidden"
	CodeRateLimited    Code = "rate_limited"
	CodePolicyDenied   Code = "policy_denied"
	CodeApprovalNeeded Code = "approval_required"
	CodeTransient      Code = "transient_error"
	CodeTimeout        Code = "timeout"
	CodeBudget         Code = "budget_exhausted"
	CodeModelOutput    Code = "model_output_invalid"
	CodeNotImplemented Code = "not_implemented"
	CodeInternal       Code = "internal_error"
)

type Error struct {
	Code    Code
	Message string
	Op      string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Op != "" && e.Err != nil {
		return e.Op + ": " + e.Err.Error()
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(code Code, message string) error {
	return &Error{Code: code, Message: message}
}

func Wrap(err error, code Code, op, message string) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Message: message, Op: op, Err: err}
}

func CodeOf(err error) Code {
	var target *Error
	if errors.As(err, &target) && target.Code != "" {
		return target.Code
	}
	return CodeInternal
}

func MessageOf(err error) string {
	var target *Error
	if errors.As(err, &target) && target.Message != "" {
		return target.Message
	}
	return "an internal error occurred"
}

func IsCode(err error, code Code) bool {
	return CodeOf(err) == code
}
