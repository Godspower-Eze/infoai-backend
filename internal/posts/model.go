package posts

import (
	"io"
	"time"

	"github.com/google/uuid"
)

type CreationMode string

const (
	CreationModeUser  CreationMode = "user"
	CreationModeAgent CreationMode = "agent"
)

type Status string

const (
	StatusDraft              Status = "draft"
	StatusScheduled          Status = "scheduled"
	StatusPublishing         Status = "publishing"
	StatusRetryWait          Status = "retry_wait"
	StatusPartiallyPublished Status = "partially_published"
	StatusPublished          Status = "published"
	StatusFailed             Status = "failed"
	StatusCancelled          Status = "cancelled"
	StatusDeleting           Status = "deleting"
	StatusDeletionFailed     Status = "deletion_failed"
)

type Post struct {
	ID           uuid.UUID
	OwnerID      uuid.UUID
	XAccountID   uuid.UUID
	CreationMode CreationMode
	Status       Status
	ScheduledAt  *time.Time
	ActiveJobID  *int64
	StateVersion int64
	Items        []Item
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Item struct {
	ID              uuid.UUID
	Position        int
	Text            string
	Media           []Media
	XPostID         *string
	SubmissionState SubmissionState
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SubmissionState string

const (
	SubmissionNotStarted     SubmissionState = "not_started"
	SubmissionSubmitting     SubmissionState = "submitting"
	SubmissionPublished      SubmissionState = "published"
	SubmissionOutcomeUnknown SubmissionState = "outcome_unknown"
)

type Media struct {
	ID               uuid.UUID
	Position         int
	StorageKey       string
	OriginalFilename string
	MIMEType         string
	Size             int64
	SHA256           []byte
	AltText          *string
	CreatedAt        time.Time
}

type ItemInput struct {
	ID    uuid.UUID
	Text  string
	Media []Media
}

type CreateCommand struct {
	OwnerID      uuid.UUID
	XAccountID   uuid.UUID
	CreationMode CreationMode
	Items        []ItemInput
}

type UpdateCommand struct {
	OwnerID uuid.UUID
	PostID  uuid.UUID
	Items   []ItemInput
}

type UploadMediaCommand struct {
	OwnerID          uuid.UUID
	PostID           uuid.UUID
	ItemID           uuid.UUID
	OriginalFilename string
	AltText          *string
	Header           []byte
	Size             int64
	Source           io.Reader
}

type AddMediaCommand struct {
	OwnerID  uuid.UUID
	PostID   uuid.UUID
	ItemID   uuid.UUID
	Media    Media
	Category MediaCategory
}

type RemoveMediaCommand struct {
	OwnerID uuid.UUID
	PostID  uuid.UUID
	ItemID  uuid.UUID
	MediaID uuid.UUID
}

type OutcomeDecision string

const (
	OutcomeNotPublished OutcomeDecision = "not_published"
	OutcomePublished    OutcomeDecision = "published"
	OutcomeUnresolved   OutcomeDecision = "unresolved"
)

type ResolveOutcomeCommand struct {
	OwnerID  uuid.UUID
	PostID   uuid.UUID
	ItemID   uuid.UUID
	Decision OutcomeDecision
	XURL     string
}

type DeleteCommand struct {
	OwnerID          uuid.UUID
	PostID           uuid.UUID
	ConfirmXDeletion bool
}

type RetryCommand struct {
	OwnerID uuid.UUID
	PostID  uuid.UUID
}
