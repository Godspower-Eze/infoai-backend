package xintegration

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
)

func withSession(t *testing.T, manager *scs.SessionManager, cookie *http.Cookie, handler http.HandlerFunc) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	manager.LoadAndSave(handler).ServeHTTP(recorder, request)
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		return recorder, cookie
	}
	return recorder, cookies[0]
}

func TestOAuthSessionBeginsAndConsumesPKCEAttempt(t *testing.T) {
	manager := scs.New()
	manager.Store = memstore.New()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	flow := NewOAuthSession(manager, func() time.Time { return now })

	var state, challenge string
	_, cookie := withSession(t, manager, nil, func(w http.ResponseWriter, r *http.Request) {
		url, err := flow.Begin(r.Context(), func(gotState, gotChallenge string) string {
			state, challenge = gotState, gotChallenge
			return "https://x.example/authorize"
		})
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		if url != "https://x.example/authorize" {
			t.Fatalf("Begin() URL = %q", url)
		}
		if state == "" || challenge == "" {
			t.Fatal("Begin() must generate state and PKCE challenge")
		}
		if stored := manager.GetString(r.Context(), oauthStateHashKey); stored == state {
			t.Fatal("session must store a hash rather than plaintext OAuth state")
		}
	})

	withSession(t, manager, cookie, func(w http.ResponseWriter, r *http.Request) {
		verifier, err := flow.Consume(r.Context(), state)
		if err != nil {
			t.Fatalf("Consume() error = %v", err)
		}
		if verifier == "" {
			t.Fatal("Consume() verifier is empty")
		}
		if got := PKCEChallenge(verifier); got != challenge {
			t.Fatalf("PKCEChallenge(verifier) = %q, want %q", got, challenge)
		}
		if _, err := flow.Consume(r.Context(), state); !errors.Is(err, ErrInvalidOAuthState) {
			t.Fatalf("replayed Consume() error = %v, want ErrInvalidOAuthState", err)
		}
	})
}

func TestOAuthSessionRejectsWrongAndExpiredState(t *testing.T) {
	manager := scs.New()
	manager.Store = memstore.New()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	flow := NewOAuthSession(manager, func() time.Time { return now })

	var state string
	_, cookie := withSession(t, manager, nil, func(w http.ResponseWriter, r *http.Request) {
		_, err := flow.Begin(r.Context(), func(gotState, _ string) string {
			state = gotState
			return "https://x.example/authorize"
		})
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
	})

	withSession(t, manager, cookie, func(w http.ResponseWriter, r *http.Request) {
		if _, err := flow.Consume(r.Context(), "wrong-state"); !errors.Is(err, ErrInvalidOAuthState) {
			t.Fatalf("wrong-state Consume() error = %v, want ErrInvalidOAuthState", err)
		}
	})

	_, cookie = withSession(t, manager, nil, func(w http.ResponseWriter, r *http.Request) {
		_, err := flow.Begin(r.Context(), func(gotState, _ string) string {
			state = gotState
			return "https://x.example/authorize"
		})
		if err != nil {
			t.Fatalf("second Begin() error = %v", err)
		}
	})
	now = now.Add(11 * time.Minute)
	withSession(t, manager, cookie, func(w http.ResponseWriter, r *http.Request) {
		if _, err := flow.Consume(r.Context(), state); !errors.Is(err, ErrInvalidOAuthState) {
			t.Fatalf("expired Consume() error = %v, want ErrInvalidOAuthState", err)
		}
	})
}
