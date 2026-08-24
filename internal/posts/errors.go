package posts

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound          = errors.New("post not found")
	ErrAccountNotFound   = errors.New("X account not found")
	ErrNotEditable       = errors.New("post is not editable")
	ErrInvalidTransition = errors.New("invalid post transition")
	ErrInvalidInput      = errors.New("invalid post input")
	ErrTextTooLong       = errors.New("post text is too long")
)

type FieldError struct {
	Field string
	Code  string
	Err   error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Code)
}

func (e *FieldError) Unwrap() error {
	if e.Err != nil {
		return e.Err
	}
	return ErrInvalidInput
}
