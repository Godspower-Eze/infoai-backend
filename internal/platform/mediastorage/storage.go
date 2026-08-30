package mediastorage

import "errors"

var (
	ErrInvalidKey  = errors.New("invalid media storage key")
	ErrInvalidRoot = errors.New("invalid media storage root")
)
