package database

import (
	"context"
	"database/sql"
	"errors"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Godspower-Eze/infoai-backend/ent"
)

func WithTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx, *ent.Client) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	driver := entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx})
	client := ent.NewClient(ent.Driver(driver))
	if err := fn(tx, client); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}
