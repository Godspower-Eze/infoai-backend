package config

import (
	"encoding/base64"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultSessionLifetime = 30 * 24 * time.Hour

type LookupFunc func(string) (string, bool)

type Config struct {
	HTTPAddress           string
	DatabaseURL           string
	APIOrigin             string
	FrontendOrigin        string
	FrontendXRedirectURL  string
	CookieSecure          bool
	SessionLifetime       time.Duration
	TokenEncryptionKey    []byte
	XClientID             string
	XClientSecret         string
	XCallbackURL          string
	TrustedProxyCIDRs     []string
	ErrorLogPath          string
	ErrorLogMaxBytes      int64
	ErrorLogRetainedFiles int
	MediaStorageRoot      string
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid configuration: " + strings.Join(e.Problems, "; ")
}

func Load(lookup LookupFunc) (Config, error) {
	var cfg Config
	var problems []string
	cfg.HTTPAddress = ":8080"
	cfg.ErrorLogPath = "var/log/infoai/errors.jsonl"
	cfg.ErrorLogMaxBytes = 10 << 20
	cfg.ErrorLogRetainedFiles = 5
	cfg.MediaStorageRoot = "var/media"
	if raw, ok := lookup("HTTP_ADDRESS"); ok && strings.TrimSpace(raw) != "" {
		cfg.HTTPAddress = strings.TrimSpace(raw)
	}

	cfg.DatabaseURL = required(lookup, "DATABASE_URL", &problems)
	cfg.APIOrigin = required(lookup, "API_ORIGIN", &problems)
	cfg.FrontendOrigin = required(lookup, "FRONTEND_ORIGIN", &problems)
	cfg.FrontendXRedirectURL = required(lookup, "FRONTEND_X_REDIRECT_URL", &problems)
	encryptionKey := required(lookup, "TOKEN_ENCRYPTION_KEY", &problems)
	cfg.XClientID = required(lookup, "X_CLIENT_ID", &problems)
	cfg.XClientSecret = required(lookup, "X_CLIENT_SECRET", &problems)
	cfg.XCallbackURL = required(lookup, "X_CALLBACK_URL", &problems)
	if raw, ok := lookup("TRUSTED_PROXY_CIDRS"); ok && strings.TrimSpace(raw) != "" {
		for _, value := range strings.Split(raw, ",") {
			prefix := strings.TrimSpace(value)
			if _, err := netip.ParsePrefix(prefix); err != nil {
				problems = append(problems, "TRUSTED_PROXY_CIDRS must contain comma-separated CIDR prefixes")
				break
			}
			cfg.TrustedProxyCIDRs = append(cfg.TrustedProxyCIDRs, prefix)
		}
	}
	if raw, ok := lookup("ERROR_LOG_PATH"); ok {
		if strings.TrimSpace(raw) == "" {
			problems = append(problems, "ERROR_LOG_PATH must not be empty")
		} else {
			cfg.ErrorLogPath = strings.TrimSpace(raw)
		}
	}
	if raw, ok := lookup("ERROR_LOG_MAX_BYTES"); ok {
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || value <= 0 {
			problems = append(problems, "ERROR_LOG_MAX_BYTES must be a positive integer")
		} else {
			cfg.ErrorLogMaxBytes = value
		}
	}
	if raw, ok := lookup("ERROR_LOG_RETAINED_FILES"); ok {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value <= 0 {
			problems = append(problems, "ERROR_LOG_RETAINED_FILES must be a positive integer")
		} else {
			cfg.ErrorLogRetainedFiles = value
		}
	}
	if raw, ok := lookup("MEDIA_STORAGE_ROOT"); ok {
		if strings.TrimSpace(raw) == "" {
			problems = append(problems, "MEDIA_STORAGE_ROOT must not be empty")
		} else {
			cfg.MediaStorageRoot = strings.TrimSpace(raw)
		}
	}

	cfg.CookieSecure = false
	if raw, ok := lookup("COOKIE_SECURE"); ok && strings.TrimSpace(raw) != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			problems = append(problems, "COOKIE_SECURE must be true or false")
		} else {
			cfg.CookieSecure = value
		}
	}

	cfg.SessionLifetime = defaultSessionLifetime
	if raw, ok := lookup("SESSION_LIFETIME"); ok && strings.TrimSpace(raw) != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			problems = append(problems, "SESSION_LIFETIME must be a positive duration")
		} else {
			cfg.SessionLifetime = value
		}
	}

	if encryptionKey != "" {
		value, err := base64.StdEncoding.DecodeString(encryptionKey)
		if err != nil || len(value) != 32 {
			problems = append(problems, "TOKEN_ENCRYPTION_KEY must be base64-encoded 32 bytes")
		} else {
			cfg.TokenEncryptionKey = value
		}
	}

	validateURL("DATABASE_URL", cfg.DatabaseURL, false, &problems)
	validateURL("API_ORIGIN", cfg.APIOrigin, true, &problems)
	validateURL("FRONTEND_ORIGIN", cfg.FrontendOrigin, true, &problems)
	validateURL("FRONTEND_X_REDIRECT_URL", cfg.FrontendXRedirectURL, false, &problems)
	validateURL("X_CALLBACK_URL", cfg.XCallbackURL, false, &problems)

	if len(problems) > 0 {
		return Config{}, &ValidationError{Problems: problems}
	}
	return cfg, nil
}

func required(lookup LookupFunc, key string, problems *[]string) string {
	value, ok := lookup(key)
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		*problems = append(*problems, fmt.Sprintf("%s is required", key))
		return ""
	}
	return value
}

func validateURL(key, value string, originOnly bool, problems *[]string) {
	if value == "" {
		return
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		*problems = append(*problems, fmt.Sprintf("%s must be an absolute URL", key))
		return
	}
	if originOnly && (parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "") {
		*problems = append(*problems, fmt.Sprintf("%s must contain only scheme and host", key))
	}
}
