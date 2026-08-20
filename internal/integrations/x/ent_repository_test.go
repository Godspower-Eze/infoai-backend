package xintegration

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/Godspower-Eze/infoai-backend/ent"
	"github.com/Godspower-Eze/infoai-backend/ent/enttest"
	_ "github.com/mattn/go-sqlite3"
)

func createOwner(t *testing.T, client *ent.Client, email string) *ent.User {
	t.Helper()
	owner, err := client.User.Create().SetEmail(email).SetPasswordHash("hash").Save(context.Background())
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	return owner
}

func TestEntAccountRepositoryUpsertsForOwnerAndRejectsOtherOwner(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:xaccounts?mode=memory&cache=shared&_fk=1")
	repository := NewEntAccountRepository(client)
	firstOwner := createOwner(t, client, "first@example.com")
	secondOwner := createOwner(t, client, "second@example.com")
	ctx := context.Background()
	firstCheckedAt := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	profile := Profile{ID: "x-123", Username: "first", DisplayName: "First", SubscriptionType: "Premium", SubscriptionCheckedAt: &firstCheckedAt}
	initialGrant := EncryptedGrant{AccessToken: []byte("cipher-one"), Expiry: time.Now().Add(time.Hour), Scopes: []string{"tweet.read"}}

	created, err := repository.Upsert(ctx, firstOwner.ID, profile, initialGrant)
	if err != nil {
		t.Fatalf("first Upsert() error = %v", err)
	}
	if created.SubscriptionType != "Premium" || created.SubscriptionCheckedAt == nil || !created.SubscriptionCheckedAt.Equal(firstCheckedAt) {
		t.Fatalf("created subscription metadata = %q/%v", created.SubscriptionType, created.SubscriptionCheckedAt)
	}
	profile.Username = "renamed"
	secondCheckedAt := firstCheckedAt.Add(time.Hour)
	profile.SubscriptionType = "FutureTier"
	profile.SubscriptionCheckedAt = &secondCheckedAt
	replacement := EncryptedGrant{AccessToken: []byte("cipher-two"), Expiry: time.Now().Add(2 * time.Hour), Scopes: []string{"tweet.read", "tweet.write"}}
	updated, err := repository.Upsert(ctx, firstOwner.ID, profile, replacement)
	if err != nil {
		t.Fatalf("owner Upsert() error = %v", err)
	}
	if updated.ID != created.ID || updated.Username != "renamed" || updated.SubscriptionType != "FutureTier" || updated.SubscriptionCheckedAt == nil || !updated.SubscriptionCheckedAt.Equal(secondCheckedAt) {
		t.Fatalf("owner Upsert() = %+v, want same account with updated metadata", updated)
	}
	stored, err := repository.Grant(ctx, firstOwner.ID, created.ID)
	if err != nil {
		t.Fatalf("Grant() error = %v", err)
	}
	if !bytes.Equal(stored.AccessToken, replacement.AccessToken) {
		t.Fatalf("stored access token = %q, want replacement", stored.AccessToken)
	}

	if _, err := repository.Upsert(ctx, secondOwner.ID, profile, replacement); !errors.Is(err, ErrAccountOwned) {
		t.Fatalf("cross-owner Upsert() error = %v, want ErrAccountOwned", err)
	}
}

func TestEntAccountRepositoryScopesReadsUpdatesAndDeletesToOwner(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:xaccount-ownership?mode=memory&cache=shared&_fk=1")
	repository := NewEntAccountRepository(client)
	owner := createOwner(t, client, "owner@example.com")
	other := createOwner(t, client, "other@example.com")
	ctx := context.Background()
	account, err := repository.Upsert(ctx, owner.ID, Profile{ID: "x-456", Username: "owner"}, EncryptedGrant{AccessToken: []byte("cipher"), Expiry: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	accounts, err := repository.List(ctx, owner.ID)
	if err != nil || len(accounts) != 1 || accounts[0].ID != account.ID {
		t.Fatalf("List() = %+v, error = %v", accounts, err)
	}
	if accounts, err := repository.List(ctx, other.ID); err != nil || len(accounts) != 0 {
		t.Fatalf("other List() = %+v, error = %v", accounts, err)
	}
	if _, err := repository.Grant(ctx, other.ID, account.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("other Grant() error = %v, want ErrAccountNotFound", err)
	}
	if err := repository.UpdateGrant(ctx, other.ID, account.ID, EncryptedGrant{}); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("other UpdateGrant() error = %v, want ErrAccountNotFound", err)
	}
	if _, err := repository.Delete(ctx, other.ID, account.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("other Delete() error = %v, want ErrAccountNotFound", err)
	}
	if _, err := repository.Delete(ctx, owner.ID, account.ID); err != nil {
		t.Fatalf("owner Delete() error = %v", err)
	}
	if _, err := repository.Grant(ctx, owner.ID, account.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("deleted Grant() error = %v, want ErrAccountNotFound", err)
	}
}
