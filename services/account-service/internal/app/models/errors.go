package models

import "errors"

var (
	ErrNotFound = errors.New("account not found")
	ErrConflict = errors.New("concurrent update conflict")
)

type InvalidError struct {
	Code    string
	Message string
}

func (e *InvalidError) Error() string {
	return e.Code + ": " + e.Message
}

func Invalid(code, message string) error {
	return &InvalidError{Code: code, Message: message}
}

func IsInvalid(err error) bool {
	var invalid *InvalidError
	return errors.As(err, &invalid)
}

func InvalidCode(err error) string {
	var invalid *InvalidError
	if errors.As(err, &invalid) {
		return invalid.Code
	}
	return ""
}
