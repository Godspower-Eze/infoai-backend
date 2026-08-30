package posts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/platform/errorreporting"
	"github.com/google/uuid"
)

const (
	FailureRetryable       = "definite_retryable"
	FailurePermanent       = "permanent"
	FailureReauthorization = "reauthorization"
	FailureAmbiguous       = "ambiguous"
)

type AccessTokenProvider interface {
	AccessToken(context.Context, uuid.UUID, uuid.UUID) (string, error)
}

type PublishCommand struct {
	PostID       uuid.UUID
	JobID        int64
	StateVersion int64
	Trigger      PublishTrigger
	Worker       string
}

type Lease struct {
	Token   uuid.UUID
	Version int64
	Post    Post
	Attempt int
}

type PublishingRepository interface {
	ClaimLease(context.Context, PublishCommand, time.Time) (Lease, error)
	MarkSubmitting(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int64) error
	MarkItemPublished(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int64, string, time.Time) error
	MarkPublished(context.Context, uuid.UUID, uuid.UUID, int64, time.Time) error
	MarkOutcomeUnknown(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int64, string) error
	ScheduleRetry(context.Context, uuid.UUID, uuid.UUID, int64, time.Time, string) error
	MarkFailed(context.Context, uuid.UUID, uuid.UUID, int64, string) error
}

type Publisher struct {
	repository PublishingRepository
	tokens     AccessTokenProvider
	x          XPublisher
	storage    MediaStorage
	retry      RetryPolicy
	reporter   errorreporting.Reporter
	now        func() time.Time
}

func NewPublisher(repository PublishingRepository, tokens AccessTokenProvider, xPublisher XPublisher, storage MediaStorage, retry RetryPolicy, reporter errorreporting.Reporter) *Publisher {
	return &Publisher{repository: repository, tokens: tokens, x: xPublisher, storage: storage, retry: retry, reporter: reporter, now: time.Now}
}

func (p *Publisher) Publish(ctx context.Context, command PublishCommand) error {
	lease, err := p.repository.ClaimLease(ctx, command, p.now())
	if err != nil {
		return err
	}
	token, err := p.tokens.AccessToken(ctx, lease.Post.OwnerID, lease.Post.XAccountID)
	if err != nil {
		return p.fail(ctx, command, lease, FailureReauthorization, time.Time{}, "access_token", err)
	}
	previous := ""
	for _, item := range lease.Post.Items {
		if item.XPostID != nil {
			previous = *item.XPostID
			continue
		}
		mediaIDs := make([]string, 0, len(item.Media))
		for _, asset := range item.Media {
			file, openErr := p.storage.Open(ctx, asset.StorageKey)
			if openErr != nil {
				return p.fail(ctx, command, lease, FailurePermanent, time.Time{}, "media_open", openErr)
			}
			mediaID, uploadErr := p.x.UploadMedia(ctx, token, MediaUpload{Filename: asset.OriginalFilename, MIMEType: asset.MIMEType, Category: categoryForMIME(asset.MIMEType), Size: asset.Size, AltText: asset.AltText, Source: file})
			closeErr := file.Close()
			if uploadErr != nil {
				classification, retryAt := classifyProviderError(uploadErr)
				if classification == FailureAmbiguous {
					classification = FailureRetryable
				}
				return p.fail(ctx, command, lease, classification, retryAt, "media_upload_failed", uploadErr)
			}
			if closeErr != nil {
				return p.fail(ctx, command, lease, FailurePermanent, time.Time{}, "media_close", closeErr)
			}
			mediaIDs = append(mediaIDs, mediaID)
		}
		if err := p.repository.MarkSubmitting(ctx, command.PostID, item.ID, lease.Token, lease.Version); err != nil {
			return err
		}
		xID, createErr := p.x.CreatePost(ctx, token, XPostInput{Text: item.Text, MediaIDs: mediaIDs, ReplyToID: previous})
		if createErr != nil {
			return p.handleXFailure(ctx, command, lease, item.ID, createErr)
		}
		if err := p.repository.MarkItemPublished(ctx, command.PostID, item.ID, lease.Token, lease.Version, xID, p.now()); err != nil {
			return err
		}
		previous = xID
	}
	return p.repository.MarkPublished(ctx, command.PostID, lease.Token, lease.Version, p.now())
}

func (p *Publisher) handleXFailure(ctx context.Context, command PublishCommand, lease Lease, itemID uuid.UUID, err error) error {
	classification, retryAt := classifyProviderError(err)
	if classification == FailureAmbiguous {
		if markErr := p.repository.MarkOutcomeUnknown(ctx, command.PostID, itemID, lease.Token, lease.Version, "ambiguous_provider_result"); markErr != nil {
			return errors.Join(err, markErr)
		}
		p.capture(ctx, command, lease, classification, err)
		return ErrOutcomeUnknown
	}
	return p.fail(ctx, command, lease, classification, retryAt, "provider_failure", err)
}

func (p *Publisher) fail(ctx context.Context, command PublishCommand, lease Lease, classification string, providerRetryAt time.Time, code string, cause error) error {
	p.capture(ctx, command, lease, classification, cause)
	if classification == FailureRetryable {
		if next, ok := p.retry.Next(lease.Attempt, p.now(), providerRetryAt); ok {
			if err := p.repository.ScheduleRetry(ctx, command.PostID, lease.Token, lease.Version, next, code); err != nil {
				return errors.Join(cause, err)
			}
			return nil
		}
	}
	if err := p.repository.MarkFailed(ctx, command.PostID, lease.Token, lease.Version, code); err != nil {
		return errors.Join(cause, err)
	}
	return fmt.Errorf("publication failed: %w", cause)
}

func (p *Publisher) capture(ctx context.Context, command PublishCommand, lease Lease, classification string, err error) {
	if p.reporter != nil {
		p.reporter.Capture(ctx, err, errorreporting.Event{Code: "publication_failed", Classification: classification, PostID: command.PostID.String(), JobID: command.JobID, Worker: command.Worker, LeaseVersion: lease.Version, RetryNumber: lease.Attempt})
	}
}

func classifyProviderError(err error) (string, time.Time) {
	var classified interface{ ProviderClassification() string }
	if !errors.As(err, &classified) {
		return FailurePermanent, time.Time{}
	}
	var retry interface{ ProviderRetryAt() time.Time }
	if errors.As(err, &retry) {
		return classified.ProviderClassification(), retry.ProviderRetryAt()
	}
	return classified.ProviderClassification(), time.Time{}
}
