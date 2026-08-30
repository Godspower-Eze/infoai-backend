package posts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/platform/errorreporting"
	"github.com/google/uuid"
)

type DeleteJobCommand struct {
	PostID       uuid.UUID
	JobID        int64
	StateVersion int64
	Worker       string
}

type DeletionWorkRepository interface {
	LoadDeletion(context.Context, uuid.UUID, int64, int64) (Post, error)
	MarkXDeleted(context.Context, uuid.UUID, uuid.UUID, int64) error
	CompleteDeletion(context.Context, uuid.UUID, int64) error
	MarkDeletionFailed(context.Context, uuid.UUID, int64, string) error
	ScheduleDeletionRetry(context.Context, uuid.UUID, int64, time.Time, string) error
}

type Deleter struct {
	repository DeletionWorkRepository
	tokens     AccessTokenProvider
	x          XPublisher
	reporter   errorreporting.Reporter
}

func NewDeleter(repository DeletionWorkRepository, tokens AccessTokenProvider, publisher XPublisher, reporter errorreporting.Reporter) *Deleter {
	return &Deleter{repository: repository, tokens: tokens, x: publisher, reporter: reporter}
}

func (d *Deleter) Delete(ctx context.Context, command DeleteJobCommand) error {
	post, err := d.repository.LoadDeletion(ctx, command.PostID, command.JobID, command.StateVersion)
	if err != nil {
		return err
	}
	token, err := d.tokens.AccessToken(ctx, post.OwnerID, post.XAccountID)
	if err != nil {
		return d.fail(ctx, command, err)
	}
	items := append([]Item(nil), post.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Position > items[j].Position })
	for _, item := range items {
		if item.XPostID == nil {
			continue
		}
		if err := d.x.DeletePost(ctx, token, *item.XPostID); err != nil {
			return d.fail(ctx, command, err)
		}
		if err := d.repository.MarkXDeleted(ctx, post.ID, item.ID, command.StateVersion); err != nil {
			return err
		}
	}
	return d.repository.CompleteDeletion(ctx, post.ID, command.StateVersion)
}

func (d *Deleter) fail(ctx context.Context, command DeleteJobCommand, cause error) error {
	if d.reporter != nil {
		d.reporter.Capture(ctx, cause, errorreporting.Event{Code: "post_deletion_failed", PostID: command.PostID.String(), JobID: command.JobID, Worker: command.Worker, LeaseVersion: command.StateVersion})
	}
	classification, retryAt := classifyProviderError(cause)
	if classification == FailureRetryable || classification == FailureAmbiguous {
		if retryAt.IsZero() {
			retryAt = time.Now().Add(time.Minute)
		}
		if err := d.repository.ScheduleDeletionRetry(ctx, command.PostID, command.StateVersion, retryAt, "x_deletion_failed"); err == nil {
			return nil
		}
	}
	return errors.Join(fmt.Errorf("delete post from X: %w", cause), d.repository.MarkDeletionFailed(ctx, command.PostID, command.StateVersion, "x_deletion_failed"))
}
