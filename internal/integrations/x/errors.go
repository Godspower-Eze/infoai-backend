package xintegration

import (
	"errors"
	"fmt"
	"time"
)

type FailureClassification string

const (
	DefiniteRetryable FailureClassification = "definite_retryable"
	Permanent         FailureClassification = "permanent"
	Reauthorization   FailureClassification = "reauthorization"
	Ambiguous         FailureClassification = "ambiguous"
)

type PublishError struct {
	Classification FailureClassification
	StatusCode     int
	RetryAt        time.Time
	Operation      string
	Cause          error
}

func (e *PublishError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("X %s failed with status %d", e.Operation, e.StatusCode)
	}
	return fmt.Sprintf("X %s failed: %v", e.Operation, e.Cause)
}
func (e *PublishError) Unwrap() error                  { return e.Cause }
func (e *PublishError) ProviderClassification() string { return string(e.Classification) }
func (e *PublishError) ProviderRetryAt() time.Time     { return e.RetryAt }

func ClassificationOf(err error) FailureClassification {
	var target *PublishError
	if errors.As(err, &target) {
		return target.Classification
	}
	return Permanent
}
