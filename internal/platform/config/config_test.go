package config_test

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/platform/config"
)

func validEnvironment() map[string]string {
	return map[string]string{
		"DATABASE_URL":            "postgres://infoai:infoai@localhost:5432/infoai?sslmode=disable",
		"API_ORIGIN":              "http://localhost:8080",
		"FRONTEND_ORIGIN":         "http://localhost:3000",
		"FRONTEND_X_REDIRECT_URL": "http://localhost:3000/settings/integrations",
		"COOKIE_SECURE":           "false",
		"SESSION_LIFETIME":        "720h",
		"TOKEN_ENCRYPTION_KEY":    base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"X_CLIENT_ID":             "x-client-id",
		"X_CLIENT_SECRET":         "x-client-secret",
		"X_CALLBACK_URL":          "http://localhost:8080/api/v1/integrations/x/callback",
	}
}

func lookup(values map[string]string) config.LookupFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestLoadAcceptsValidEnvironment(t *testing.T) {
	cfg, err := config.Load(lookup(validEnvironment()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.SessionLifetime != 30*24*time.Hour {
		t.Fatalf("SessionLifetime = %v, want %v", cfg.SessionLifetime, 30*24*time.Hour)
	}
	if cfg.CookieSecure {
		t.Fatal("CookieSecure = true, want false")
	}
	if len(cfg.TokenEncryptionKey) != 32 {
		t.Fatalf("TokenEncryptionKey length = %d, want 32", len(cfg.TokenEncryptionKey))
	}
	if cfg.HTTPAddress != ":8080" {
		t.Fatalf("HTTPAddress = %q, want :8080", cfg.HTTPAddress)
	}
	if cfg.ErrorLogPath != "var/log/infoai/errors.jsonl" || cfg.ErrorLogMaxBytes != 10<<20 || cfg.ErrorLogRetainedFiles != 5 {
		t.Fatalf("error log defaults = %q/%d/%d", cfg.ErrorLogPath, cfg.ErrorLogMaxBytes, cfg.ErrorLogRetainedFiles)
	}
}

func TestLoadRejectsInvalidErrorLogConfiguration(t *testing.T) {
	environment := validEnvironment()
	environment["ERROR_LOG_MAX_BYTES"] = "0"
	environment["ERROR_LOG_RETAINED_FILES"] = "many"
	_, err := config.Load(lookup(environment))
	if err == nil || !strings.Contains(err.Error(), "ERROR_LOG_MAX_BYTES") || !strings.Contains(err.Error(), "ERROR_LOG_RETAINED_FILES") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadAcceptsCustomHTTPAddress(t *testing.T) {
	environment := validEnvironment()
	environment["HTTP_ADDRESS"] = "127.0.0.1:9090"

	cfg, err := config.Load(lookup(environment))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddress != "127.0.0.1:9090" {
		t.Fatalf("HTTPAddress = %q", cfg.HTTPAddress)
	}
}

func TestLoadAcceptsTrustedProxyCIDRs(t *testing.T) {
	environment := validEnvironment()
	environment["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8, 2001:db8::/32"

	cfg, err := config.Load(lookup(environment))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := cfg.TrustedProxyCIDRs, []string{"10.0.0.0/8", "2001:db8::/32"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TrustedProxyCIDRs = %v, want %v", got, want)
	}
}

func TestLoadRejectsInvalidTrustedProxyCIDR(t *testing.T) {
	environment := validEnvironment()
	environment["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8,not-a-prefix"

	_, err := config.Load(lookup(environment))
	if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Fatalf("Load() error = %v, want trusted-proxy validation error", err)
	}
}

func TestLoadReportsAllMissingRequiredValues(t *testing.T) {
	_, err := config.Load(lookup(map[string]string{}))
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}

	validationErr, ok := err.(*config.ValidationError)
	if !ok {
		t.Fatalf("Load() error type = %T, want *config.ValidationError", err)
	}
	if got, want := len(validationErr.Problems), 8; got != want {
		t.Fatalf("problem count = %d, want %d: %v", got, want, validationErr.Problems)
	}
}

func TestLoadRejectsMalformedSecurityAndURLValues(t *testing.T) {
	environment := validEnvironment()
	environment["COOKIE_SECURE"] = "sometimes"
	environment["SESSION_LIFETIME"] = "a while"
	environment["TOKEN_ENCRYPTION_KEY"] = base64.StdEncoding.EncodeToString(make([]byte, 16))
	environment["FRONTEND_ORIGIN"] = "/relative"

	_, err := config.Load(lookup(environment))
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}

	validationErr, ok := err.(*config.ValidationError)
	if !ok {
		t.Fatalf("Load() error type = %T, want *config.ValidationError", err)
	}
	if got, want := len(validationErr.Problems), 4; got != want {
		t.Fatalf("problem count = %d, want %d: %v", got, want, validationErr.Problems)
	}
}
