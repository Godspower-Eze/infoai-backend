package errorreporting

import "context"

type Reporter interface {
	Capture(context.Context, error, Event)
	Close() error
}

type Event struct {
	Code           string `json:"code"`
	Classification string `json:"classification,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
	PostID         string `json:"post_id,omitempty"`
	ItemID         string `json:"item_id,omitempty"`
	AttemptID      string `json:"attempt_id,omitempty"`
	JobID          int64  `json:"job_id,omitempty"`
	Worker         string `json:"worker,omitempty"`
	LeaseVersion   int64  `json:"lease_version,omitempty"`
	RetryNumber    int    `json:"retry_number,omitempty"`
	ProviderStatus int    `json:"provider_status,omitempty"`
}
