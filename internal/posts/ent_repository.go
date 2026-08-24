package posts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godspower-Eze/infoai-backend/ent"
	"github.com/Godspower-Eze/infoai-backend/ent/mediaasset"
	entpost "github.com/Godspower-Eze/infoai-backend/ent/post"
	"github.com/Godspower-Eze/infoai-backend/ent/postitem"
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
	if _, err := tx.PostItem.Delete().Where(postitem.PostIDEQ(postID)).Exec(ctx); err != nil {
		return Post{}, fmt.Errorf("delete replaced post items: %w", err)
	}
	if err := createEntItems(ctx, tx, postID, items); err != nil {
		return Post{}, err
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
		ScheduledAt: stored.ScheduledAt, CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt,
		Items: make([]Item, len(items)),
	}
	for index, storedItem := range items {
		storedMedia, mediaErr := storedItem.Edges.MediaAssetsOrErr()
		if mediaErr != nil {
			return Post{}, fmt.Errorf("load post item media: %w", mediaErr)
		}
		item := Item{
			ID: storedItem.ID, Position: storedItem.Position, Text: storedItem.Text,
			XPostID: storedItem.XPostID, CreatedAt: storedItem.CreatedAt, UpdatedAt: storedItem.UpdatedAt,
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
