package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godspowere/infoai-backend/internal/platform/config"
)

func TestLoadDotEnvAllowsMissingFile(t *testing.T) {
	err := config.LoadDotEnv(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}
}

func TestLoadDotEnvLoadsValuesWithoutOverwritingEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("INFOAI_DOTENV_NEW=from-file\nINFOAI_DOTENV_EXISTING=from-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	unsetEnvironmentForTest(t, "INFOAI_DOTENV_NEW")
	t.Setenv("INFOAI_DOTENV_EXISTING", "from-process")

	if err := config.LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}
	if got := os.Getenv("INFOAI_DOTENV_NEW"); got != "from-file" {
		t.Fatalf("INFOAI_DOTENV_NEW = %q, want from-file", got)
	}
	if got := os.Getenv("INFOAI_DOTENV_EXISTING"); got != "from-process" {
		t.Fatalf("INFOAI_DOTENV_EXISTING = %q, want from-process", got)
	}
}

func TestLoadDotEnvRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("INVALID LINE WITHOUT ASSIGNMENT\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := config.LoadDotEnv(path)
	if err == nil || !strings.Contains(err.Error(), "load dotenv") {
		t.Fatalf("LoadDotEnv() error = %v, want contextual parse error", err)
	}
}

func unsetEnvironmentForTest(t *testing.T, key string) {
	t.Helper()
	value, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv() error = %v", err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
