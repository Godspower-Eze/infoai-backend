package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Godspower-Eze/infoai-backend/ent"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestWithTxRollsBackEntChangesWhenCallbackFails(t *testing.T) {
	db := openTestSQLDB(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	errAbort := errors.New("abort transaction")

	err := WithTx(ctx, db, func(_ *sql.Tx, client *ent.Client) error {
		if _, err := client.User.Create().
			SetEmail(email).
			SetPasswordHash("test-password-hash").
			Save(ctx); err != nil {
			return err
		}
		return errAbort
	})
	if !errors.Is(err, errAbort) {
		t.Fatalf("WithTx() error = %v, want %v", err, errAbort)
	}
	assertUserCount(t, db, email, 0)
}

func TestWithTxCommitsEntChangesWhenCallbackSucceeds(t *testing.T) {
	db := openTestSQLDB(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)

	if err := WithTx(ctx, db, func(_ *sql.Tx, client *ent.Client) error {
		_, err := client.User.Create().
			SetEmail(email).
			SetPasswordHash("test-password-hash").
			Save(ctx)
		return err
	}); err != nil {
		t.Fatalf("WithTx() error = %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE email = $1`, email); err != nil {
			t.Errorf("delete test user: %v", err)
		}
	})
	assertUserCount(t, db, email, 1)
}

func openTestSQLDB(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("PingContext() error = %v", err)
	}
	return db
}

func uniqueTestEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("transaction-%d@example.com", time.Now().UnixNano())
}

func assertUserCount(t *testing.T, db *sql.DB, email string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE email = $1`, email).Scan(&count); err != nil {
		t.Fatalf("query user count: %v", err)
	}
	if count != want {
		t.Fatalf("user count = %d, want %d", count, want)
	}
}
