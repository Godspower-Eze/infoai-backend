package migrate_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMigrationsContainInitialAndPublishingSchemas(t *testing.T) {
	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("migration file count = %d, want 2", len(files))
	}
	if !regexp.MustCompile(`/[0-9]{14}_initial_schema\.sql$`).MatchString(filepath.ToSlash(files[0])) {
		t.Fatalf("migration name = %q, want timestamped initial_schema file", files[0])
	}
	if !regexp.MustCompile(`/[0-9]{14}_add_post_publishing\.sql$`).MatchString(filepath.ToSlash(files[1])) {
		t.Fatalf("migration name = %q, want timestamped add_post_publishing file", files[1])
	}

	initialContents, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, table := range []string{"users", "x_accounts", "sessions"} {
		if !strings.Contains(string(initialContents), `CREATE TABLE "`+table+`"`) {
			t.Errorf("migration does not create %s table", table)
		}
	}

	publishingContents, err := os.ReadFile(files[1])
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, table := range []string{"posts", "post_items", "media_assets", "storage_deletions", "publication_attempts"} {
		if !strings.Contains(string(publishingContents), `CREATE TABLE "`+table+`"`) {
			t.Errorf("publishing migration does not create %s table", table)
		}
	}

	if _, err := os.Stat("migrations/atlas.sum"); err != nil {
		t.Fatalf("atlas.sum missing: %v", err)
	}
}
