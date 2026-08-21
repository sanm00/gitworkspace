package gitworkspace

import (
	"errors"
	"fmt"
)

type AppError struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.Cause }

func NewError(code, message string) *AppError {
	return &AppError{Code: code, Message: message, Details: map[string]any{}}
}

func WrapError(code, message string, cause error) *AppError {
	return &AppError{Code: code, Message: message, Details: map[string]any{}, Cause: cause}
}

func ErrorCode(err error) string {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return "internal_error"
}

func ErrorDetails(err error) map[string]any {
	var appErr *AppError
	if errors.As(err, &appErr) && appErr.Details != nil {
		return appErr.Details
	}
	return map[string]any{}
}
