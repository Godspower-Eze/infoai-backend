package posts

import (
	"context"

	"github.com/google/uuid"
)

type Repository interface {
	AccountSubscription(context.Context, uuid.UUID, uuid.UUID) (string, error)
	Create(context.Context, CreateCommand) (Post, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Post, error)
	List(context.Context, uuid.UUID) ([]Post, error)
	ReplaceItems(context.Context, uuid.UUID, uuid.UUID, []ItemInput) (Post, error)
}
