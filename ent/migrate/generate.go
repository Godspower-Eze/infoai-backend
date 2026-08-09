//go:build ignore

package main

import (
	"context"
	"log"
	"os"

	"github.com/godspowere/infoai-backend/ent/migrate"

	atlas "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/lib/pq"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run ./ent/migrate/generate.go <migration_name>")
	}
	devURL := os.Getenv("ATLAS_DEV_DATABASE_URL")
	if devURL == "" {
		log.Fatal("ATLAS_DEV_DATABASE_URL is required")
	}

	if err := os.MkdirAll("ent/migrate/migrations", 0o755); err != nil {
		log.Fatalf("create migration directory: %v", err)
	}
	dir, err := atlas.NewLocalDir("ent/migrate/migrations")
	if err != nil {
		log.Fatalf("open migration directory: %v", err)
	}
	options := []schema.MigrateOption{
		schema.WithDir(dir),
		schema.WithMigrationMode(schema.ModeReplay),
		schema.WithDialect(dialect.Postgres),
		schema.WithFormatter(atlas.DefaultFormatter),
	}
	if err := migrate.NamedDiff(context.Background(), devURL, os.Args[1], options...); err != nil {
		log.Fatalf("generate migration: %v", err)
	}
}
