package posts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godspower-Eze/infoai-backend/ent"
	"github.com/Godspower-Eze/infoai-backend/ent/mediaasset"
	entpost "github.com/Godspower-Eze/infoai-backend/ent/post"
	"github.com/Godspower-Eze/infoai-backend/ent/postitem"
	"github.com/Godspower-Eze/infoai-backend/ent/storagedeletion"
	"github.com/Godspower-Eze/infoai-backend/ent/user"
	"github.com/Godspower-Eze/infoai-backend/ent/xaccount"
	"github.com/google/uuid"
)

type EntPostRepository struct {
	client *ent.Client
}

func NewEntPostRepository(client *ent.Client) *EntPostRepository {
	return &EntPostRepository{client: client}
}

func (r *EntPostRepository) AccountSubscription(ctx context.Context, ownerID, accountID uuid.UUID) (string, error) {
	account, err := r.client.XAccount.Query().Where(xaccount.IDEQ(accountID), xaccount.HasOwnerWith(user.IDEQ(ownerID))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", ErrAccountNotFound
		}
		return "", fmt.Errorf("find owned X account: %w", err)
	}
	return account.SubscriptionType, nil
}

func (r *EntPostRepository) Create(ctx context.Context, command CreateCommand) (result Post, returnedErr error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("begin post creation: %w", err)
	}
	defer func() {
		if returnedErr != nil {
			returnedErr = errors.Join(returnedErr, tx.Rollback())
		}
	}()

	if _, err := tx.XAccount.Query().Where(xaccount.IDEQ(command.XAccountID), xaccount.HasOwnerWith(user.IDEQ(command.OwnerID))).Only(ctx); err != nil {
		if ent.IsNotFound(err) {
			return Post{}, ErrAccountNotFound
		}
		return Post{}, fmt.Errorf("validate post X account: %w", err)
	}
	created, err := tx.Post.Create().
		SetOwnerID(command.OwnerID).
		SetXAccountID(command.XAccountID).
		SetCreationMode(entpost.CreationMode(command.CreationMode)).
		Save(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("create post: %w", err)
	}
	if err := createEntItems(ctx, tx, created.ID, command.Items); err != nil {
		return Post{}, err
	}
	stored, err := queryPost(tx.Post.Query(), command.OwnerID, created.ID).Only(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("load created post: %w", err)
	}
	result, err = postFromEnt(stored)
	if err != nil {
		return Post{}, err
	}
	if err := tx.Commit(); err != nil {
		return Post{}, fmt.Errorf("commit post creation: %w", err)
	}
	return result, nil
}

func (r *EntPostRepository) Get(ctx context.Context, ownerID, postID uuid.UUID) (Post, error) {
	stored, err := queryPost(r.client.Post.Query(), ownerID, postID).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return Post{}, ErrNotFound
		}
		return Post{}, fmt.Errorf("get post: %w", err)
	}
	return postFromEnt(stored)
}

func (r *EntPostRepository) List(ctx context.Context, ownerID uuid.UUID) ([]Post, error) {
	stored, err := queryPosts(r.client.Post.Query(), ownerID).
		Order(ent.Desc(entpost.FieldCreatedAt), ent.Desc(entpost.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	result := make([]Post, len(stored))
	for index, item := range stored {
		result[index], err = postFromEnt(item)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *EntPostRepository) ReplaceItems(ctx context.Context, ownerID, postID uuid.UUID, items []ItemInput) (result Post, returnedErr error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("begin post item replacement: %w", err)
	}
	defer func() {
		if returnedErr != nil {
			returnedErr = errors.Join(returnedErr, tx.Rollback())
		}
	}()

	updated, err := tx.Post.Update().Where(
		entpost.IDEQ(postID),
		entpost.OwnerIDEQ(ownerID),
		entpost.StatusIn(entpost.StatusDraft, entpost.StatusScheduled),
	).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("claim editable post: %w", err)
	}
	if updated == 0 {
		exists, lookupErr := tx.Post.Query().Where(entpost.IDEQ(postID), entpost.OwnerIDEQ(ownerID)).Exist(ctx)
		if lookupErr != nil {
			return Post{}, fmt.Errorf("inspect uneditable post: %w", lookupErr)
		}
		if !exists {
			return Post{}, ErrNotFound
		}
		return Post{}, ErrNotEditable
	}
	existing, err := tx.PostItem.Query().Where(postitem.PostIDEQ(postID)).WithMediaAssets().All(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("load replaced post items: %w", err)
	}
	byID := make(map[uuid.UUID]*ent.PostItem, len(existing))
	for _, item := range existing {
		byID[item.ID] = item
	}
	retained := make(map[uuid.UUID]bool, len(items))
	for _, input := range items {
		if input.ID != uuid.Nil {
			if byID[input.ID] == nil || retained[input.ID] {
				return Post{}, &FieldError{Field: "items", Code: "invalid_id"}
			}
			retained[input.ID] = true
		}
	}
	for _, item := range existing {
		if retained[item.ID] {
			continue
		}
		media, _ := item.Edges.MediaAssetsOrErr()
		for _, asset := range media {
			if err := queueStorageDeletion(ctx, tx, asset.StorageKey); err != nil {
				return Post{}, err
			}
		}
		if err := tx.PostItem.DeleteOneID(item.ID).Exec(ctx); err != nil {
			return Post{}, fmt.Errorf("delete omitted post item: %w", err)
		}
	}
	if _, err := tx.PostItem.Update().Where(postitem.PostIDEQ(postID)).AddPosition(1000).Save(ctx); err != nil {
		return Post{}, fmt.Errorf("prepare post item ordering: %w", err)
	}
	for index, input := range items {
		if input.ID == uuid.Nil {
			if _, err := tx.PostItem.Create().SetPostID(postID).SetPosition(index).SetText(input.Text).Save(ctx); err != nil {
				return Post{}, fmt.Errorf("create replacement post item: %w", err)
			}
			continue
		}
		if err := tx.PostItem.UpdateOneID(input.ID).SetPosition(index).SetText(input.Text).Exec(ctx); err != nil {
			return Post{}, fmt.Errorf("update retained post item: %w", err)
		}
	}
	stored, err := queryPost(tx.Post.Query(), ownerID, postID).Only(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("load updated post: %w", err)
	}
	result, err = postFromEnt(stored)
	if err != nil {
		return Post{}, err
	}
	if err := tx.Commit(); err != nil {
		return Post{}, fmt.Errorf("commit post item replacement: %w", err)
	}
	return result, nil
}

func (r *EntPostRepository) AddMedia(ctx context.Context, command AddMediaCommand) (result Post, returnedErr error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return Post{}, err
	}
	defer func() {
		if returnedErr != nil {
			returnedErr = errors.Join(returnedErr, tx.Rollback())
		}
	}()
	status, err := claimEditablePost(ctx, tx, command.OwnerID, command.PostID)
	if err != nil {
		return Post{}, err
	}
	_ = status
	item, err := tx.PostItem.Query().Where(postitem.IDEQ(command.ItemID), postitem.PostIDEQ(command.PostID)).WithMediaAssets(func(q *ent.MediaAssetQuery) { q.Order(ent.Asc(mediaasset.FieldPosition)) }).Only(ctx)
	if ent.IsNotFound(err) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, err
	}
	storedMedia, _ := item.Edges.MediaAssetsOrErr()
	siblings := make([]MediaMetadata, len(storedMedia))
	for index, asset := range storedMedia {
		siblings[index] = MediaMetadata{Category: categoryForMIME(asset.MimeType)}
	}
	if err := validateMediaCombination(command.Category, siblings); err != nil {
		return Post{}, err
	}
	media := command.Media
	_, err = tx.MediaAsset.Create().SetID(media.ID).SetOwnerID(command.OwnerID).SetPostItemID(command.ItemID).SetPosition(len(storedMedia)).SetStorageKey(media.StorageKey).SetOriginalFilename(media.OriginalFilename).SetMimeType(media.MIMEType).SetSizeBytes(media.Size).SetSha256Checksum(media.SHA256).SetNillableAltText(media.AltText).Save(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("persist media metadata: %w", err)
	}
	stored, err := queryPost(tx.Post.Query(), command.OwnerID, command.PostID).Only(ctx)
	if err != nil {
		return Post{}, err
	}
	result, err = postFromEnt(stored)
	if err != nil {
		return Post{}, err
	}
	if err := tx.Commit(); err != nil {
		return Post{}, err
	}
	return result, nil
}

func (r *EntPostRepository) RemoveMedia(ctx context.Context, command RemoveMediaCommand) (returnedErr error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if returnedErr != nil {
			returnedErr = errors.Join(returnedErr, tx.Rollback())
		}
	}()
	status, err := claimEditablePost(ctx, tx, command.OwnerID, command.PostID)
	if err != nil {
		return err
	}
	item, err := tx.PostItem.Query().Where(postitem.IDEQ(command.ItemID), postitem.PostIDEQ(command.PostID)).WithMediaAssets().Only(ctx)
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	media, _ := item.Edges.MediaAssetsOrErr()
	var target *ent.MediaAsset
	for _, asset := range media {
		if asset.ID == command.MediaID {
			target = asset
			break
		}
	}
	if target == nil {
		return ErrNotFound
	}
	if Status(status) == StatusScheduled && strings.TrimSpace(item.Text) == "" && len(media) == 1 {
		return &FieldError{Field: "items[" + fmt.Sprint(item.Position) + "]", Code: "content_required"}
	}
	if err := queueStorageDeletion(ctx, tx, target.StorageKey); err != nil {
		return err
	}
	if err := tx.MediaAsset.DeleteOneID(target.ID).Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *EntPostRepository) Delete(ctx context.Context, ownerID, postID uuid.UUID) (returnedErr error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if returnedErr != nil {
			returnedErr = errors.Join(returnedErr, tx.Rollback())
		}
	}()
	updated, err := tx.Post.Update().Where(entpost.IDEQ(postID), entpost.OwnerIDEQ(ownerID), entpost.StatusIn(entpost.StatusDraft, entpost.StatusScheduled)).AddLeaseVersion(1).ClearActiveRiverJobID().SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return err
	}
	if updated == 0 {
		exists, _ := tx.Post.Query().Where(entpost.IDEQ(postID), entpost.OwnerIDEQ(ownerID)).Exist(ctx)
		if !exists {
			return ErrNotFound
		}
		return ErrNotEditable
	}
	assets, err := tx.MediaAsset.Query().Where(mediaasset.OwnerIDEQ(ownerID), mediaasset.HasPostItemWith(postitem.PostIDEQ(postID))).All(ctx)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if err := queueStorageDeletion(ctx, tx, asset.StorageKey); err != nil {
			return err
		}
	}
	if err := tx.Post.DeleteOneID(postID).Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *EntPostRepository) QueueStorageDeletion(ctx context.Context, key string) error {
	return queueStorageDeletion(ctx, r.client, key)
}

func (r *EntPostRepository) ClaimLease(ctx context.Context, command PublishCommand, now time.Time) (Lease, error) {
	token := uuid.New()
	expires := now.Add(2 * time.Minute)
	count, err := r.client.Post.Update().Where(
		entpost.IDEQ(command.PostID), entpost.ActiveRiverJobIDEQ(command.JobID), entpost.LeaseVersionEQ(command.StateVersion),
		entpost.StatusIn(entpost.StatusPublishing, entpost.StatusScheduled, entpost.StatusRetryWait),
		entpost.Or(entpost.LeaseExpiresAtIsNil(), entpost.LeaseExpiresAtLTE(now)),
	).SetStatus(entpost.StatusPublishing).SetLeaseToken(token).SetLeaseOwner(command.Worker).SetLeaseExpiresAt(expires).AddAttemptCount(1).Save(ctx)
	if err != nil {
		return Lease{}, fmt.Errorf("claim publication lease: %w", err)
	}
	if count != 1 {
		return Lease{}, ErrLeaseLost
	}
	stored, err := r.client.Post.Query().Where(entpost.IDEQ(command.PostID)).WithItems(func(q *ent.PostItemQuery) {
		q.Order(ent.Asc(postitem.FieldPosition)).WithMediaAssets(func(m *ent.MediaAssetQuery) { m.Order(ent.Asc(mediaasset.FieldPosition)) })
	}).Only(ctx)
	if err != nil {
		return Lease{}, err
	}
	post, err := postFromEnt(stored)
	if err != nil {
		return Lease{}, err
	}
	return Lease{Token: token, Version: stored.LeaseVersion, Post: post, Attempt: stored.AttemptCount}, nil
}

func (r *EntPostRepository) MarkSubmitting(ctx context.Context, postID, itemID, token uuid.UUID, version int64) error {
	return r.updateLeasedItem(ctx, postID, itemID, token, version, func(update *ent.PostItemUpdate) *ent.PostItemUpdate {
		return update.SetSubmissionState(postitem.SubmissionStateSubmitting).SetSubmissionStartedAt(time.Now())
	})
}

func (r *EntPostRepository) MarkItemPublished(ctx context.Context, postID, itemID, token uuid.UUID, version int64, xID string, at time.Time) error {
	return r.updateLeasedItem(ctx, postID, itemID, token, version, func(update *ent.PostItemUpdate) *ent.PostItemUpdate {
		return update.SetSubmissionState(postitem.SubmissionStatePublished).SetXPostID(xID).SetPublishedAt(at)
	})
}

func (r *EntPostRepository) MarkOutcomeUnknown(ctx context.Context, postID, itemID, token uuid.UUID, version int64, code string) error {
	if err := r.updateLeasedItem(ctx, postID, itemID, token, version, func(update *ent.PostItemUpdate) *ent.PostItemUpdate {
		return update.SetSubmissionState(postitem.SubmissionStateOutcomeUnknown)
	}); err != nil {
		return err
	}
	return r.finishLease(ctx, postID, token, version, entpost.StatusPartiallyPublished, code, nil)
}

func (r *EntPostRepository) MarkPublished(ctx context.Context, postID, token uuid.UUID, version int64, at time.Time) error {
	count, err := r.leasedPostUpdate(postID, token, version).SetStatus(entpost.StatusPublished).SetPublishedAt(at).ClearActiveRiverJobID().ClearLeaseToken().ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *EntPostRepository) ScheduleRetry(ctx context.Context, postID, token uuid.UUID, version int64, at time.Time, code string) error {
	return r.finishLease(ctx, postID, token, version, entpost.StatusRetryWait, code, &at)
}

func (r *EntPostRepository) MarkFailed(ctx context.Context, postID, token uuid.UUID, version int64, code string) error {
	return r.finishLease(ctx, postID, token, version, entpost.StatusFailed, code, nil)
}

func (r *EntPostRepository) updateLeasedItem(ctx context.Context, postID, itemID, token uuid.UUID, version int64, mutate func(*ent.PostItemUpdate) *ent.PostItemUpdate) error {
	owned, err := r.client.Post.Query().Where(entpost.IDEQ(postID), entpost.LeaseTokenEQ(token), entpost.LeaseVersionEQ(version), entpost.LeaseExpiresAtGT(time.Now())).Exist(ctx)
	if err != nil {
		return err
	}
	if !owned {
		return ErrLeaseLost
	}
	count, err := mutate(r.client.PostItem.Update().Where(postitem.IDEQ(itemID), postitem.PostIDEQ(postID))).Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *EntPostRepository) leasedPostUpdate(postID, token uuid.UUID, version int64) *ent.PostUpdate {
	return r.client.Post.Update().Where(entpost.IDEQ(postID), entpost.LeaseTokenEQ(token), entpost.LeaseVersionEQ(version), entpost.LeaseExpiresAtGT(time.Now()))
}

func (r *EntPostRepository) finishLease(ctx context.Context, postID, token uuid.UUID, version int64, status entpost.Status, code string, retryAt *time.Time) error {
	update := r.leasedPostUpdate(postID, token, version).SetStatus(status).SetLastErrorCode(code).ClearActiveRiverJobID().ClearLeaseToken().ClearLeaseOwner().ClearLeaseExpiresAt().SetNillableNextAttemptAt(retryAt)
	count, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *EntPostRepository) LoadDeletion(ctx context.Context, postID uuid.UUID, jobID, version int64) (Post, error) {
	count, err := r.client.Post.Update().Where(entpost.IDEQ(postID), entpost.StatusEQ(entpost.StatusDeleting), entpost.ActiveRiverJobIDEQ(jobID), entpost.LeaseVersionEQ(version)).AddAttemptCount(1).Save(ctx)
	if err != nil {
		return Post{}, err
	}
	if count != 1 {
		return Post{}, ErrLeaseLost
	}
	stored, err := r.client.Post.Query().Where(entpost.IDEQ(postID), entpost.StatusEQ(entpost.StatusDeleting), entpost.ActiveRiverJobIDEQ(jobID), entpost.LeaseVersionEQ(version)).WithItems(func(q *ent.PostItemQuery) {
		q.Order(ent.Asc(postitem.FieldPosition)).WithMediaAssets(func(m *ent.MediaAssetQuery) { m.Order(ent.Asc(mediaasset.FieldPosition)) })
	}).Only(ctx)
	if ent.IsNotFound(err) {
		return Post{}, ErrLeaseLost
	}
	if err != nil {
		return Post{}, err
	}
	return postFromEnt(stored)
}

func (r *EntPostRepository) ScheduleDeletionRetry(ctx context.Context, postID uuid.UUID, version int64, at time.Time, code string) error {
	stored, err := r.client.Post.Query().Where(entpost.IDEQ(postID), entpost.StatusEQ(entpost.StatusDeleting), entpost.LeaseVersionEQ(version)).Only(ctx)
	if ent.IsNotFound(err) {
		return ErrLeaseLost
	}
	if err != nil {
		return err
	}
	if stored.AttemptCount >= 3 {
		return errors.New("deletion retry exhausted")
	}
	count, err := r.client.Post.Update().Where(entpost.IDEQ(postID), entpost.StatusEQ(entpost.StatusDeleting), entpost.LeaseVersionEQ(version)).SetStatus(entpost.StatusDeletionFailed).SetNextAttemptAt(at).SetLastErrorCode(code).ClearActiveRiverJobID().Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *EntPostRepository) MarkXDeleted(ctx context.Context, postID, itemID uuid.UUID, version int64) error {
	exists, err := r.client.Post.Query().Where(entpost.IDEQ(postID), entpost.StatusEQ(entpost.StatusDeleting), entpost.LeaseVersionEQ(version)).Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrLeaseLost
	}
	count, err := r.client.PostItem.Update().Where(postitem.IDEQ(itemID), postitem.PostIDEQ(postID)).ClearXPostID().Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *EntPostRepository) CompleteDeletion(ctx context.Context, postID uuid.UUID, version int64) (returnedErr error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if returnedErr != nil {
			returnedErr = errors.Join(returnedErr, tx.Rollback())
		}
	}()
	stored, err := tx.Post.Query().Where(entpost.IDEQ(postID), entpost.StatusEQ(entpost.StatusDeleting), entpost.LeaseVersionEQ(version)).Only(ctx)
	if ent.IsNotFound(err) {
		return ErrLeaseLost
	}
	if err != nil {
		return err
	}
	assets, err := tx.MediaAsset.Query().Where(mediaasset.HasPostItemWith(postitem.PostIDEQ(stored.ID))).All(ctx)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if err := queueStorageDeletion(ctx, tx, asset.StorageKey); err != nil {
			return err
		}
	}
	if err := tx.Post.DeleteOneID(postID).Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *EntPostRepository) MarkDeletionFailed(ctx context.Context, postID uuid.UUID, version int64, code string) error {
	count, err := r.client.Post.Update().Where(entpost.IDEQ(postID), entpost.StatusEQ(entpost.StatusDeleting), entpost.LeaseVersionEQ(version)).SetStatus(entpost.StatusDeletionFailed).SetLastErrorCode(code).ClearActiveRiverJobID().Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *EntPostRepository) CompleteStorageDeletion(ctx context.Context, key string) error {
	_, err := r.client.StorageDeletion.Delete().Where(storagedeletion.StorageKeyEQ(key)).Exec(ctx)
	return err
}

func (r *EntPostRepository) FailStorageDeletion(ctx context.Context, key, code string) error {
	stored, err := r.client.StorageDeletion.Query().Where(storagedeletion.StorageKeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	update := r.client.StorageDeletion.UpdateOneID(stored.ID).AddAttemptCount(1)
	if stored.AttemptCount+1 >= 3 {
		update.SetLastErrorCode("storage_delete_exhausted").ClearNextAttemptAt()
	} else {
		update.SetLastErrorCode(code).SetNextAttemptAt(time.Now().Add(time.Minute))
	}
	err = update.Exec(ctx)
	return err
}

type storageDeletionClient interface {
	StorageDeletionQuery() *ent.StorageDeletionQuery
}

func queueStorageDeletion(ctx context.Context, client interface{}, key string) error {
	var query *ent.StorageDeletionQuery
	var create *ent.StorageDeletionCreate
	switch value := client.(type) {
	case *ent.Client:
		query, create = value.StorageDeletion.Query(), value.StorageDeletion.Create()
	case *ent.Tx:
		query, create = value.StorageDeletion.Query(), value.StorageDeletion.Create()
	default:
		return errors.New("unsupported storage deletion client")
	}
	exists, err := query.Where(storagedeletion.StorageKeyEQ(key)).Exist(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := create.SetStorageKey(key).Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			return nil
		}
		return fmt.Errorf("queue storage deletion: %w", err)
	}
	return nil
}

func claimEditablePost(ctx context.Context, tx *ent.Tx, ownerID, postID uuid.UUID) (entpost.Status, error) {
	updated, err := tx.Post.Update().Where(entpost.IDEQ(postID), entpost.OwnerIDEQ(ownerID), entpost.StatusIn(entpost.StatusDraft, entpost.StatusScheduled)).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return "", err
	}
	if updated == 0 {
		exists, _ := tx.Post.Query().Where(entpost.IDEQ(postID), entpost.OwnerIDEQ(ownerID)).Exist(ctx)
		if !exists {
			return "", ErrNotFound
		}
		return "", ErrNotEditable
	}
	stored, err := tx.Post.Get(ctx, postID)
	if err != nil {
		return "", err
	}
	return stored.Status, nil
}

func queryPost(query *ent.PostQuery, ownerID, postID uuid.UUID) *ent.PostQuery {
	return queryPosts(query, ownerID).Where(entpost.IDEQ(postID))
}

func queryPosts(query *ent.PostQuery, ownerID uuid.UUID) *ent.PostQuery {
	return query.
		Where(entpost.OwnerIDEQ(ownerID)).
		WithItems(func(query *ent.PostItemQuery) {
			query.Order(ent.Asc(postitem.FieldPosition)).
				WithMediaAssets(func(media *ent.MediaAssetQuery) { media.Order(ent.Asc(mediaasset.FieldPosition)) })
		})
}

func createEntItems(ctx context.Context, client *ent.Tx, postID uuid.UUID, items []ItemInput) error {
	builders := make([]*ent.PostItemCreate, len(items))
	for index, item := range items {
		builder := client.PostItem.Create().SetPostID(postID).SetPosition(index).SetText(item.Text)
		if item.ID != uuid.Nil {
			builder.SetID(item.ID)
		}
		builders[index] = builder
	}
	if _, err := client.PostItem.CreateBulk(builders...).Save(ctx); err != nil {
		return fmt.Errorf("create post items: %w", err)
	}
	return nil
}

func postFromEnt(stored *ent.Post) (Post, error) {
	items, err := stored.Edges.ItemsOrErr()
	if err != nil {
		return Post{}, fmt.Errorf("load post items: %w", err)
	}
	result := Post{
		ID: stored.ID, OwnerID: stored.OwnerID, XAccountID: stored.XAccountID,
		CreationMode: CreationMode(stored.CreationMode), Status: Status(stored.Status),
		ScheduledAt: stored.ScheduledAt, ActiveJobID: stored.ActiveRiverJobID, StateVersion: stored.LeaseVersion,
		CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt,
		Items: make([]Item, len(items)),
	}
	for index, storedItem := range items {
		storedMedia, mediaErr := storedItem.Edges.MediaAssetsOrErr()
		if mediaErr != nil {
			return Post{}, fmt.Errorf("load post item media: %w", mediaErr)
		}
		item := Item{
			ID: storedItem.ID, Position: storedItem.Position, Text: storedItem.Text,
			XPostID: storedItem.XPostID, SubmissionState: SubmissionState(storedItem.SubmissionState), CreatedAt: storedItem.CreatedAt, UpdatedAt: storedItem.UpdatedAt,
			Media: make([]Media, len(storedMedia)),
		}
		for mediaIndex, storedAsset := range storedMedia {
			item.Media[mediaIndex] = Media{
				ID: storedAsset.ID, Position: storedAsset.Position, StorageKey: storedAsset.StorageKey,
				OriginalFilename: storedAsset.OriginalFilename, MIMEType: storedAsset.MimeType,
				Size: storedAsset.SizeBytes, SHA256: append([]byte(nil), storedAsset.Sha256Checksum...),
				AltText: storedAsset.AltText, CreatedAt: storedAsset.CreatedAt,
			}
		}
		result.Items[index] = item
	}
	return result, nil
}

var _ Repository = (*EntPostRepository)(nil)
var _ FullRepository = (*EntPostRepository)(nil)
var _ PublishingRepository = (*EntPostRepository)(nil)
var _ DeletionWorkRepository = (*EntPostRepository)(nil)
var _ CleanupRepository = (*EntPostRepository)(nil)
