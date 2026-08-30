package posts

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Godspower-Eze/infoai-backend/ent"
	entpost "github.com/Godspower-Eze/infoai-backend/ent/post"
	"github.com/Godspower-Eze/infoai-backend/ent/postitem"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/database"
	"github.com/google/uuid"
)

type EntPublicationRepository struct {
	db    *sql.DB
	queue PublicationQueue
}

func NewEntPublicationRepository(db *sql.DB, queue PublicationQueue) *EntPublicationRepository {
	return &EntPublicationRepository{db: db, queue: queue}
}

func (r *EntPublicationRepository) Publish(ctx context.Context, ownerID, postID uuid.UUID) (Post, error) {
	return r.queuePublication(ctx, ownerID, postID, nil, false)
}

func (r *EntPublicationRepository) Schedule(ctx context.Context, ownerID, postID uuid.UUID, at time.Time) (Post, error) {
	return r.queuePublication(ctx, ownerID, postID, &at, true)
}

func (r *EntPublicationRepository) queuePublication(ctx context.Context, ownerID, postID uuid.UUID, at *time.Time, allowScheduled bool) (result Post, err error) {
	err = database.WithTx(ctx, r.db, func(tx *sql.Tx, client *ent.Client) error {
		states := []entpost.Status{entpost.StatusDraft}
		if allowScheduled {
			states = append(states, entpost.StatusScheduled)
		}
		stored, err := queryPost(client.Post.Query(), ownerID, postID).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load post for publication: %w", err)
		}
		if !containsPostStatus(states, stored.Status) {
			return ErrNotEditable
		}
		domain, err := postFromEnt(stored)
		if err != nil {
			return err
		}
		if err := ValidatePublishable(domain); err != nil {
			return err
		}
		version := stored.LeaseVersion + 1
		trigger := TriggerImmediate
		status := entpost.StatusPublishing
		if at != nil {
			trigger, status = TriggerScheduled, entpost.StatusScheduled
		}
		jobID, err := r.queue.Publish(ctx, tx, PublishArgs{PostID: postID, Trigger: trigger, StateVersion: version, ScheduledAt: at})
		if err != nil {
			return err
		}
		update := client.Post.Update().Where(entpost.IDEQ(postID), entpost.OwnerIDEQ(ownerID), entpost.StatusIn(states...), entpost.LeaseVersionEQ(stored.LeaseVersion)).
			SetStatus(status).SetLeaseVersion(version).SetActiveRiverJobID(jobID).SetNillableScheduledAt(at)
		if at == nil {
			update.ClearScheduledAt().SetPublishRequestedAt(time.Now())
		}
		count, err := update.Save(ctx)
		if err != nil {
			return fmt.Errorf("transition post for publication: %w", err)
		}
		if count != 1 {
			return ErrNotEditable
		}
		stored, err = queryPost(client.Post.Query(), ownerID, postID).Only(ctx)
		if err != nil {
			return err
		}
		result, err = postFromEnt(stored)
		return err
	})
	return result, err
}

func (r *EntPublicationRepository) CancelSchedule(ctx context.Context, ownerID, postID uuid.UUID) (result Post, err error) {
	err = database.WithTx(ctx, r.db, func(_ *sql.Tx, client *ent.Client) error {
		stored, err := client.Post.Query().Where(entpost.IDEQ(postID), entpost.OwnerIDEQ(ownerID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if stored.Status != entpost.StatusScheduled {
			return ErrNotEditable
		}
		count, err := client.Post.Update().Where(entpost.IDEQ(postID), entpost.OwnerIDEQ(ownerID), entpost.StatusEQ(entpost.StatusScheduled), entpost.LeaseVersionEQ(stored.LeaseVersion)).
			SetStatus(entpost.StatusDraft).AddLeaseVersion(1).ClearScheduledAt().ClearActiveRiverJobID().Save(ctx)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrNotEditable
		}
		updated, err := queryPost(client.Post.Query(), ownerID, postID).Only(ctx)
		if err != nil {
			return err
		}
		result, err = postFromEnt(updated)
		return err
	})
	return result, err
}

func containsPostStatus(states []entpost.Status, target entpost.Status) bool {
	for _, state := range states {
		if state == target {
			return true
		}
	}
	return false
}

var _ PublicationRepository = (*EntPublicationRepository)(nil)

func (r *EntPublicationRepository) ResolveOutcome(ctx context.Context, command ResolveOutcomeCommand, xID string) (result Post, err error) {
	err = database.WithTx(ctx, r.db, func(tx *sql.Tx, client *ent.Client) error {
		stored, err := queryPost(client.Post.Query(), command.OwnerID, command.PostID).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		items, _ := stored.Edges.ItemsOrErr()
		var target *ent.PostItem
		for _, item := range items {
			if item.ID == command.ItemID {
				target = item
				break
			}
		}
		if target == nil {
			return ErrNotFound
		}
		if target.SubmissionState != postitem.SubmissionStateOutcomeUnknown {
			return ErrInvalidTransition
		}
		itemUpdate := client.PostItem.UpdateOneID(target.ID).SetOutcomeConfirmedAt(time.Now()).SetOutcomeConfirmedBy(command.OwnerID)
		if command.Decision == OutcomePublished {
			itemUpdate.SetSubmissionState(postitem.SubmissionStatePublished).SetXPostID(xID).SetPublishedAt(time.Now()).SetConfirmedXPostURL(command.XURL)
		} else {
			itemUpdate.SetSubmissionState(postitem.SubmissionStateNotStarted).ClearSubmissionStartedAt()
		}
		if err := itemUpdate.Exec(ctx); err != nil {
			if ent.IsConstraintError(err) {
				return &FieldError{Field: "x_url", Code: "duplicate"}
			}
			return err
		}
		version := stored.LeaseVersion + 1
		jobID, err := r.queue.Publish(ctx, tx, PublishArgs{PostID: command.PostID, Trigger: TriggerRecovery, StateVersion: version})
		if err != nil {
			return err
		}
		count, err := client.Post.Update().Where(entpost.IDEQ(command.PostID), entpost.OwnerIDEQ(command.OwnerID), entpost.LeaseVersionEQ(stored.LeaseVersion)).SetStatus(entpost.StatusPublishing).SetLeaseVersion(version).SetActiveRiverJobID(jobID).SetAttemptCount(0).ClearLastErrorCode().Save(ctx)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrInvalidTransition
		}
		updated, err := queryPost(client.Post.Query(), command.OwnerID, command.PostID).Only(ctx)
		if err != nil {
			return err
		}
		result, err = postFromEnt(updated)
		return err
	})
	return result, err
}

var _ OutcomeRepository = (*EntPublicationRepository)(nil)

func (r *EntPublicationRepository) RequestDeletion(ctx context.Context, command DeleteCommand) (queued bool, err error) {
	err = database.WithTx(ctx, r.db, func(tx *sql.Tx, client *ent.Client) error {
		stored, err := queryPost(client.Post.Query(), command.OwnerID, command.PostID).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if stored.Status == entpost.StatusDeleting {
			queued = true
			return nil
		}
		if stored.LeaseExpiresAt != nil && stored.LeaseExpiresAt.After(time.Now()) {
			return ErrNotEditable
		}
		hasX := false
		items, _ := stored.Edges.ItemsOrErr()
		for _, item := range items {
			if item.XPostID != nil {
				hasX = true
				break
			}
		}
		if !hasX {
			return ErrInvalidTransition
		}
		if !command.ConfirmXDeletion {
			return ErrConfirmationRequired
		}
		version := stored.LeaseVersion + 1
		jobID, err := r.queue.DeletePublished(ctx, tx, DeletePostArgs{PostID: command.PostID, StateVersion: version})
		if err != nil {
			return err
		}
		count, err := client.Post.Update().Where(entpost.IDEQ(command.PostID), entpost.OwnerIDEQ(command.OwnerID), entpost.LeaseVersionEQ(stored.LeaseVersion)).SetStatus(entpost.StatusDeleting).SetLeaseVersion(version).SetActiveRiverJobID(jobID).SetAttemptCount(0).ClearNextAttemptAt().Save(ctx)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrInvalidTransition
		}
		queued = true
		return nil
	})
	return queued, err
}

var _ PublishedDeletionRepository = (*EntPublicationRepository)(nil)

func (r *EntPublicationRepository) Retry(ctx context.Context, command RetryCommand) (result Post, err error) {
	err = database.WithTx(ctx, r.db, func(tx *sql.Tx, client *ent.Client) error {
		stored, err := queryPost(client.Post.Query(), command.OwnerID, command.PostID).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if stored.Status != entpost.StatusFailed && stored.Status != entpost.StatusPartiallyPublished {
			return ErrInvalidTransition
		}
		items, _ := stored.Edges.ItemsOrErr()
		for _, item := range items {
			if item.SubmissionState == postitem.SubmissionStateOutcomeUnknown {
				return ErrOutcomeUnknown
			}
		}
		version := stored.LeaseVersion + 1
		jobID, err := r.queue.Publish(ctx, tx, PublishArgs{PostID: command.PostID, Trigger: TriggerRetry, StateVersion: version})
		if err != nil {
			return err
		}
		count, err := client.Post.Update().Where(entpost.IDEQ(command.PostID), entpost.OwnerIDEQ(command.OwnerID), entpost.LeaseVersionEQ(stored.LeaseVersion)).SetStatus(entpost.StatusPublishing).SetLeaseVersion(version).SetActiveRiverJobID(jobID).SetAttemptCount(0).ClearLastErrorCode().ClearNextAttemptAt().Save(ctx)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrInvalidTransition
		}
		updated, err := queryPost(client.Post.Query(), command.OwnerID, command.PostID).Only(ctx)
		if err != nil {
			return err
		}
		result, err = postFromEnt(updated)
		return err
	})
	return result, err
}

var _ RetryRepository = (*EntPublicationRepository)(nil)
