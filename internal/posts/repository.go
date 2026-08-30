package posts

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type PublicationRepository interface {
	Publish(context.Context, uuid.UUID, uuid.UUID) (Post, error)
	Schedule(context.Context, uuid.UUID, uuid.UUID, time.Time) (Post, error)
	CancelSchedule(context.Context, uuid.UUID, uuid.UUID) (Post, error)
}

type OutcomeRepository interface {
	ResolveOutcome(context.Context, ResolveOutcomeCommand, string) (Post, error)
}

type PublishedDeletionRepository interface {
	RequestDeletion(context.Context, DeleteCommand) (bool, error)
}

type RetryRepository interface {
	Retry(context.Context, RetryCommand) (Post, error)
}

type Repository interface {
	AccountSubscription(context.Context, uuid.UUID, uuid.UUID) (string, error)
	Create(context.Context, CreateCommand) (Post, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Post, error)
	List(context.Context, uuid.UUID) ([]Post, error)
	ReplaceItems(context.Context, uuid.UUID, uuid.UUID, []ItemInput) (Post, error)
}

type DraftDeletionRepository interface {
	Delete(context.Context, uuid.UUID, uuid.UUID) error
}

type MediaRepository interface {
	AddMedia(context.Context, AddMediaCommand) (Post, error)
	RemoveMedia(context.Context, RemoveMediaCommand) error
	QueueStorageDeletion(context.Context, string) error
}

type FullRepository interface {
	Repository
	DraftDeletionRepository
	MediaRepository
}
