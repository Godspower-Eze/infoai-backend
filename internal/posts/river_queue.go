package posts

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/riverqueue/river"
)

type PublicationQueue interface {
	Publish(context.Context, *sql.Tx, PublishArgs) (int64, error)
	DeletePublished(context.Context, *sql.Tx, DeletePostArgs) (int64, error)
	CleanupStorage(context.Context, *sql.Tx, StorageCleanupArgs) (int64, error)
}

type RiverQueue struct {
	client *river.Client[*sql.Tx]
}

func NewRiverQueue(client *river.Client[*sql.Tx]) *RiverQueue {
	return &RiverQueue{client: client}
}

func (q *RiverQueue) SetClient(client *river.Client[*sql.Tx]) { q.client = client }

func (q *RiverQueue) Publish(ctx context.Context, tx *sql.Tx, args PublishArgs) (int64, error) {
	return q.insert(ctx, tx, args, args.InsertOpts())
}

func (q *RiverQueue) DeletePublished(ctx context.Context, tx *sql.Tx, args DeletePostArgs) (int64, error) {
	return q.insert(ctx, tx, args, args.InsertOpts())
}

func (q *RiverQueue) CleanupStorage(ctx context.Context, tx *sql.Tx, args StorageCleanupArgs) (int64, error) {
	return q.insert(ctx, tx, args, args.InsertOpts())
}

func (q *RiverQueue) insert(ctx context.Context, tx *sql.Tx, args river.JobArgs, opts river.InsertOpts) (int64, error) {
	result, err := q.client.InsertTx(ctx, tx, args, &opts)
	if err != nil {
		return 0, fmt.Errorf("insert River job %q: %w", args.Kind(), err)
	}
	return result.Job.ID, nil
}

var _ PublicationQueue = (*RiverQueue)(nil)
