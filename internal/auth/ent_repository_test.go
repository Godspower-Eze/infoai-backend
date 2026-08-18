package auth

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/Godspower-Eze/infoai-backend/ent/enttest"
	_ "github.com/mattn/go-sqlite3"
)

func TestEntUserRepositoryPersistsAndFindsUsers(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:auth?mode=memory&cache=shared&_fk=1")
	repository := NewEntUserRepository(client)
	ctx := context.Background()

	created, err := repository.Create(ctx, "person@example.com", "encoded-password")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	credentials, err := repository.ByEmail(ctx, "person@example.com")
	if err != nil {
		t.Fatalf("ByEmail() error = %v", err)
	}
	if credentials.ID != created.ID || credentials.PasswordHash != "encoded-password" {
		t.Fatalf("ByEmail() = %+v, want created user and password hash", credentials)
	}
	byID, err := repository.ByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("ByID() error = %v", err)
	}
	if byID.ID != created.ID || byID.Email != created.Email || !byID.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("ByID() = %+v, want %+v", byID, created)
	}
}

func TestEntUserRepositoryMapsStorageErrors(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:auth-errors?mode=memory&cache=shared&_fk=1")
	repository := NewEntUserRepository(client)
	ctx := context.Background()

	if _, err := repository.Create(ctx, "person@example.com", "encoded-password"); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if _, err := repository.Create(ctx, "person@example.com", "other-password"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate Create() error = %v, want ErrEmailTaken", err)
	}
	if _, err := repository.ByEmail(ctx, "missing@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing ByEmail() error = %v, want ErrUserNotFound", err)
	}
}
