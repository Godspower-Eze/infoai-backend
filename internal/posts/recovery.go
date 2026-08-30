package posts

import (
	"context"
	"database/sql"
	"time"

	"github.com/Godspower-Eze/infoai-backend/ent"
	entpost "github.com/Godspower-Eze/infoai-backend/ent/post"
	"github.com/Godspower-Eze/infoai-backend/ent/postitem"
	"github.com/Godspower-Eze/infoai-backend/ent/storagedeletion"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/database"
)

type Recovery struct {
	db        *sql.DB
	queue     PublicationQueue
	now       func() time.Time
	batchSize int
}

func NewRecovery(db *sql.DB, queue PublicationQueue) *Recovery {
	return &Recovery{db: db, queue: queue, now: time.Now, batchSize: 100}
}

func (r *Recovery) Run(ctx context.Context, _ RecoverArgs) error {
	now := r.now()
	return database.WithTx(ctx, r.db, func(tx *sql.Tx, client *ent.Client) error {
		candidates, err := client.Post.Query().Where(entpost.Or(
			entpost.And(entpost.StatusEQ(entpost.StatusScheduled), entpost.ScheduledAtLTE(now)),
			entpost.And(entpost.StatusEQ(entpost.StatusRetryWait), entpost.NextAttemptAtLTE(now)),
			entpost.And(entpost.StatusEQ(entpost.StatusPublishing), entpost.LeaseExpiresAtLTE(now)),
		)).Order(ent.Asc(entpost.FieldUpdatedAt), ent.Asc(entpost.FieldID)).Limit(r.batchSize).All(ctx)
		if err != nil {
			return err
		}
		for _, post := range candidates {
			if post.Status == entpost.StatusPublishing {
				ambiguous, err := client.PostItem.Query().Where(postitem.PostIDEQ(post.ID), postitem.SubmissionStateEQ(postitem.SubmissionStateSubmitting)).Exist(ctx)
				if err != nil {
					return err
				}
				if ambiguous {
					if _, err := client.PostItem.Update().Where(postitem.PostIDEQ(post.ID), postitem.SubmissionStateEQ(postitem.SubmissionStateSubmitting)).SetSubmissionState(postitem.SubmissionStateOutcomeUnknown).Save(ctx); err != nil {
						return err
					}
					if _, err := client.Post.Update().Where(entpost.IDEQ(post.ID), entpost.LeaseVersionEQ(post.LeaseVersion)).SetStatus(entpost.StatusPartiallyPublished).ClearLeaseToken().ClearLeaseOwner().ClearLeaseExpiresAt().ClearActiveRiverJobID().Save(ctx); err != nil {
						return err
					}
					continue
				}
			}
			version := post.LeaseVersion + 1
			count, err := client.Post.Update().Where(entpost.IDEQ(post.ID), entpost.LeaseVersionEQ(post.LeaseVersion), entpost.StatusEQ(post.Status)).SetStatus(entpost.StatusPublishing).SetLeaseVersion(version).ClearActiveRiverJobID().ClearLeaseToken().ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx)
			if err != nil {
				return err
			}
			if count != 1 {
				continue
			}
			jobID, err := r.queue.Publish(ctx, tx, PublishArgs{PostID: post.ID, Trigger: TriggerRecovery, StateVersion: version})
			if err != nil {
				return err
			}
			if _, err := client.Post.Update().Where(entpost.IDEQ(post.ID), entpost.LeaseVersionEQ(version)).SetActiveRiverJobID(jobID).Save(ctx); err != nil {
				return err
			}
		}
		failedDeletions, err := client.Post.Query().Where(entpost.StatusEQ(entpost.StatusDeletionFailed), entpost.NextAttemptAtLTE(now), entpost.AttemptCountLT(3)).Order(ent.Asc(entpost.FieldNextAttemptAt), ent.Asc(entpost.FieldID)).Limit(r.batchSize).All(ctx)
		if err != nil {
			return err
		}
		for _, post := range failedDeletions {
			version := post.LeaseVersion + 1
			count, err := client.Post.Update().Where(entpost.IDEQ(post.ID), entpost.StatusEQ(entpost.StatusDeletionFailed), entpost.LeaseVersionEQ(post.LeaseVersion)).SetStatus(entpost.StatusDeleting).SetLeaseVersion(version).ClearNextAttemptAt().Save(ctx)
			if err != nil {
				return err
			}
			if count != 1 {
				continue
			}
			jobID, err := r.queue.DeletePublished(ctx, tx, DeletePostArgs{PostID: post.ID, StateVersion: version})
			if err != nil {
				return err
			}
			if _, err := client.Post.Update().Where(entpost.IDEQ(post.ID), entpost.LeaseVersionEQ(version)).SetActiveRiverJobID(jobID).Save(ctx); err != nil {
				return err
			}
		}
		deletions, err := client.StorageDeletion.Query().Where(storagedeletion.And(storagedeletion.Or(storagedeletion.LastErrorCodeIsNil(), storagedeletion.LastErrorCodeNEQ("storage_delete_exhausted")), storagedeletion.Or(storagedeletion.NextAttemptAtIsNil(), storagedeletion.NextAttemptAtLTE(now)))).Order(ent.Asc(storagedeletion.FieldCreatedAt), ent.Asc(storagedeletion.FieldID)).Limit(r.batchSize).All(ctx)
		if err != nil {
			return err
		}
		for _, deletion := range deletions {
			if _, err := r.queue.CleanupStorage(ctx, tx, StorageCleanupArgs{StorageKey: deletion.StorageKey}); err != nil {
				return err
			}
			if _, err := client.StorageDeletion.Update().Where(storagedeletion.IDEQ(deletion.ID), storagedeletion.AttemptCountEQ(deletion.AttemptCount)).SetNextAttemptAt(now.Add(time.Minute)).Save(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
