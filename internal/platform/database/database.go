package database

import (
	"context"
	"database/sql"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/godspowere/infoai-backend/ent"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

type Connections struct {
	Pool      *pgxpool.Pool
	SQL       *sql.DB
	EntClient *ent.Client
}

func Open(ctx context.Context, databaseURL string) (*Connections, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	driver := entsql.OpenDB("postgres", sqlDB)
	return &Connections{Pool: pool, SQL: sqlDB, EntClient: ent.NewClient(ent.Driver(driver))}, nil
}

func (c *Connections) Close() error {
	err := c.EntClient.Close()
	c.Pool.Close()
	return err
}
