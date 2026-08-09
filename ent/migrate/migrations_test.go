package migrate_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInitialMigrationContainsCompleteSchema(t *testing.T) {
	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("migration file count = %d, want 1", len(files))
	}
	if !regexp.MustCompile(`/[0-9]{14}_initial_schema\.sql$`).MatchString(filepath.ToSlash(files[0])) {
		t.Fatalf("migration name = %q, want timestamped initial_schema file", files[0])
	}

	contents, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, table := range []string{"users", "x_accounts", "sessions"} {
		if !strings.Contains(string(contents), `CREATE TABLE "`+table+`"`) {
			t.Errorf("migration does not create %s table", table)
		}
	}

	if _, err := os.Stat("migrations/atlas.sum"); err != nil {
		t.Fatalf("atlas.sum missing: %v", err)
	}
}
