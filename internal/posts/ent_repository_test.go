package posts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Godspower-Eze/infoai-backend/ent"
	"github.com/Godspower-Eze/infoai-backend/ent/post"
	"github.com/Godspower-Eze/infoai-backend/ent/storagedeletion"
	"github.com/Godspower-Eze/infoai-backend/ent/user"
	"github.com/Godspower-Eze/infoai-backend/ent/xaccount"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/database"
	"github.com/google/uuid"
)

func TestEntPostRepositoryScopesAccountAndPostsToOwner(t *testing.T) {
	client := openPostTestClient(t)
	repository := NewEntPostRepository(client)
	owner := createPostTestOwner(t, client)
	other := createPostTestOwner(t, client)
	account := createPostTestAccount(t, client, owner.ID, "Premium")
	ctx := context.Background()

	if subscription, err := repository.AccountSubscription(ctx, owner.ID, account.ID); err != nil || subscription != "Premium" {
		t.Fatalf("AccountSubscription() = %q, error = %v", subscription, err)
	}
	if _, err := repository.AccountSubscription(ctx, other.ID, account.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("other AccountSubscription() error = %v, want ErrAccountNotFound", err)
	}

	created, err := repository.Create(ctx, CreateCommand{OwnerID: owner.ID, XAccountID: account.ID, CreationMode: CreationModeUser, Items: []ItemInput{{Text: "owned"}}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := repository.Get(ctx, other.ID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other Get() error = %v, want ErrNotFound", err)
	}
	if posts, err := repository.List(ctx, other.ID); err != nil || len(posts) != 0 {
		t.Fatalf("other List() = %+v, error = %v", posts, err)
	}
}

func TestEntPostRepositoryCreatesAggregateAtomicallyInStableOrder(t *testing.T) {
	client := openPostTestClient(t)
	repository := NewEntPostRepository(client)
	owner := createPostTestOwner(t, client)
	account := createPostTestAccount(t, client, owner.ID, "None")
	ctx := context.Background()

	created, err := repository.Create(ctx, CreateCommand{OwnerID: owner.ID, XAccountID: account.ID, CreationMode: CreationModeAgent, Items: []ItemInput{{Text: "first"}, {Text: "second"}}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.CreationMode != CreationModeAgent || len(created.Items) != 2 || created.Items[0].Position != 0 || created.Items[0].Text != "first" || created.Items[1].Position != 1 {
		t.Fatalf("Create() = %+v", created)
	}

	duplicateID := uuid.New()
	_, err = repository.Create(ctx, CreateCommand{OwnerID: owner.ID, XAccountID: account.ID, CreationMode: CreationModeUser, Items: []ItemInput{{ID: duplicateID}, {ID: duplicateID}}})
	if err == nil {
		t.Fatal("Create() duplicate item error = nil")
	}
	count, countErr := client.Post.Query().Where(post.OwnerIDEQ(owner.ID)).Count(ctx)
	if countErr != nil {
		t.Fatal(countErr)
	}
	if count != 1 {
		t.Fatalf("post count = %d, want only successful aggregate", count)
	}
}

func TestEntPostRepositoryListsNewestFirstWithOrderedItems(t *testing.T) {
	client := openPostTestClient(t)
	repository := NewEntPostRepository(client)
	owner := createPostTestOwner(t, client)
	account := createPostTestAccount(t, client, owner.ID, "None")
	ctx := context.Background()

	first, err := repository.Create(ctx, CreateCommand{OwnerID: owner.ID, XAccountID: account.ID, CreationMode: CreationModeUser, Items: []ItemInput{{Text: "one"}, {Text: "two"}}})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	second, err := repository.Create(ctx, CreateCommand{OwnerID: owner.ID, XAccountID: account.ID, CreationMode: CreationModeUser, Items: []ItemInput{{Text: "newest"}}})
	if err != nil {
		t.Fatal(err)
	}

	listed, err := repository.List(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != second.ID || listed[1].ID != first.ID || listed[1].Items[0].Text != "one" || listed[1].Items[1].Text != "two" {
		t.Fatalf("List() = %+v", listed)
	}
}

func TestEntPostRepositoryReplacesOnlyEditableOwnedItems(t *testing.T) {
	client := openPostTestClient(t)
	repository := NewEntPostRepository(client)
	owner := createPostTestOwner(t, client)
	other := createPostTestOwner(t, client)
	account := createPostTestAccount(t, client, owner.ID, "None")
	ctx := context.Background()
	created, err := repository.Create(ctx, CreateCommand{OwnerID: owner.ID, XAccountID: account.ID, CreationMode: CreationModeUser, Items: []ItemInput{{Text: "old one"}, {Text: "old two"}}})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := repository.ReplaceItems(ctx, owner.ID, created.ID, []ItemInput{{Text: "replacement"}})
	if err != nil {
		t.Fatalf("ReplaceItems() error = %v", err)
	}
	if len(updated.Items) != 1 || updated.Items[0].Text != "replacement" || updated.Items[0].Position != 0 {
		t.Fatalf("ReplaceItems() = %+v", updated)
	}
	if _, err := repository.ReplaceItems(ctx, other.ID, created.ID, []ItemInput{{Text: "stolen"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other ReplaceItems() error = %v, want ErrNotFound", err)
	}
	if err := client.Post.UpdateOneID(created.ID).SetStatus(post.StatusPublished).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReplaceItems(ctx, owner.ID, created.ID, []ItemInput{{Text: "late"}}); !errors.Is(err, ErrNotEditable) {
		t.Fatalf("published ReplaceItems() error = %v, want ErrNotEditable", err)
	}
}

func TestEntPostRepositoryReplacementPreservesRetainedMediaAndQueuesOmittedMedia(t *testing.T) {
	client := openPostTestClient(t)
	repository := NewEntPostRepository(client)
	owner := createPostTestOwner(t, client)
	account := createPostTestAccount(t, client, owner.ID, "None")
	ctx := context.Background()
	created, err := repository.Create(ctx, CreateCommand{OwnerID: owner.ID, XAccountID: account.ID, CreationMode: CreationModeUser, Items: []ItemInput{{Text: "keep"}, {Text: "remove"}}})
	if err != nil {
		t.Fatal(err)
	}
	keepKey, removeKey := "media/keep-"+uuid.NewString(), "media/remove-"+uuid.NewString()
	createPostTestMedia(t, client, owner.ID, created.Items[0].ID, keepKey)
	createPostTestMedia(t, client, owner.ID, created.Items[1].ID, removeKey)
	updated, err := repository.ReplaceItems(ctx, owner.ID, created.ID, []ItemInput{{ID: created.Items[0].ID, Text: "kept"}, {Text: "new"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Items[0].ID != created.Items[0].ID || len(updated.Items[0].Media) != 1 || updated.Items[0].Media[0].StorageKey != keepKey {
		t.Fatalf("retained item = %+v", updated.Items[0])
	}
	if exists, _ := client.StorageDeletion.Query().Where(storagedeletion.StorageKeyEQ(removeKey)).Exist(ctx); !exists {
		t.Fatal("omitted media cleanup record missing")
	}
}

func TestEntPostRepositoryAddsRemovesAndDeletesMediaWithDurableCleanup(t *testing.T) {
	client := openPostTestClient(t)
	repository := NewEntPostRepository(client)
	owner := createPostTestOwner(t, client)
	account := createPostTestAccount(t, client, owner.ID, "None")
	ctx := context.Background()
	created, err := repository.Create(ctx, CreateCommand{OwnerID: owner.ID, XAccountID: account.ID, CreationMode: CreationModeUser, Items: []ItemInput{{Text: "draft"}}})
	if err != nil {
		t.Fatal(err)
	}
	key := "media/" + uuid.NewString()
	mediaID := uuid.New()
	updated, err := repository.AddMedia(ctx, AddMediaCommand{OwnerID: owner.ID, PostID: created.ID, ItemID: created.Items[0].ID, Category: MediaImage, Media: Media{ID: mediaID, StorageKey: key, OriginalFilename: "image.png", MIMEType: "image/png", Size: 8, SHA256: make([]byte, 32)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Items[0].Media) != 1 {
		t.Fatalf("media = %+v", updated.Items[0].Media)
	}
	if err := repository.RemoveMedia(ctx, RemoveMediaCommand{OwnerID: owner.ID, PostID: created.ID, ItemID: created.Items[0].ID, MediaID: mediaID}); err != nil {
		t.Fatal(err)
	}
	if exists, _ := client.StorageDeletion.Query().Where(storagedeletion.StorageKeyEQ(key)).Exist(ctx); !exists {
		t.Fatal("removed media cleanup record missing")
	}

	secondKey := "media/" + uuid.NewString()
	createPostTestMedia(t, client, owner.ID, created.Items[0].ID, secondKey)
	if err := repository.Delete(ctx, owner.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, owner.ID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted Get() error = %v", err)
	}
	if exists, _ := client.StorageDeletion.Query().Where(storagedeletion.StorageKeyEQ(secondKey)).Exist(ctx); !exists {
		t.Fatal("draft deletion cleanup record missing")
	}
}

func createPostTestMedia(t *testing.T, client *ent.Client, ownerID, itemID uuid.UUID, key string) *ent.MediaAsset {
	t.Helper()
	stored, err := client.MediaAsset.Create().SetOwnerID(ownerID).SetPostItemID(itemID).SetPosition(0).SetStorageKey(key).SetOriginalFilename("image.png").SetMimeType("image/png").SetSizeBytes(8).SetSha256Checksum(make([]byte, 32)).Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func openPostTestClient(t *testing.T) *ent.Client {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	connections, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = connections.Close() })
	return connections.EntClient
}

func createPostTestOwner(t *testing.T, client *ent.Client) *ent.User {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	owner, err := client.User.Create().SetID(id).SetEmail(fmt.Sprintf("posts-%s@example.com", id)).SetPasswordHash("hash").Save(ctx)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = client.Post.Delete().Where(post.OwnerIDEQ(id)).Exec(ctx)
		_, _ = client.XAccount.Delete().Where(xaccount.HasOwnerWith(user.IDEQ(id))).Exec(ctx)
		_ = client.User.DeleteOneID(id).Exec(ctx)
	})
	return owner
}

func createPostTestAccount(t *testing.T, client *ent.Client, ownerID uuid.UUID, subscription string) *ent.XAccount {
	t.Helper()
	id := uuid.New()
	account, err := client.XAccount.Create().
		SetID(id).
		SetOwnerID(ownerID).
		SetXUserID("x-" + id.String()).
		SetUsername("user_" + id.String()[:8]).
		SetSubscriptionType(subscription).
		SetAccessToken([]byte("ciphertext")).
		SetTokenExpiry(time.Now().Add(time.Hour)).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create X account: %v", err)
	}
	return account
}
