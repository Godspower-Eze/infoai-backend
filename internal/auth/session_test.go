package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/google/uuid"
)

func TestSessionManagerUsesSecureApplicationDefaults(t *testing.T) {
	manager := NewSessionManager(memstore.New(), 30*24*time.Hour, true)

	if manager.Cookie.Name != "infoai_session" {
		t.Fatalf("cookie name = %q, want infoai_session", manager.Cookie.Name)
	}
	if !manager.Cookie.HttpOnly || !manager.Cookie.Secure || manager.Cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie security settings = %+v", manager.Cookie)
	}
	if manager.Cookie.Path != "/" || manager.Lifetime != 30*24*time.Hour || !manager.HashTokenInStore {
		t.Fatalf("session settings = path %q, lifetime %v, hash tokens %v", manager.Cookie.Path, manager.Lifetime, manager.HashTokenInStore)
	}
}

func TestSignInRenewsSessionAndStoresUserID(t *testing.T) {
	manager := NewSessionManager(memstore.New(), time.Hour, false)
	userID := uuid.New()

	initial := httptest.NewRecorder()
	manager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manager.Put(r.Context(), "anonymous", "value")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(initial, httptest.NewRequest(http.MethodPost, "/", nil))
	firstCookie := initial.Result().Cookies()[0]

	signedIn := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.AddCookie(firstCookie)
	manager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := SignInSession(r.Context(), manager, userID); err != nil {
			t.Fatalf("SignInSession() error = %v", err)
		}
		got, err := CurrentUserID(r.Context(), manager)
		if err != nil {
			t.Fatalf("CurrentUserID() error = %v", err)
		}
		if got != userID {
			t.Fatalf("CurrentUserID() = %v, want %v", got, userID)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(signedIn, request)
	secondCookie := signedIn.Result().Cookies()[0]

	if firstCookie.Value == secondCookie.Value {
		t.Fatal("session token was not renewed during sign in")
	}
}

func TestSignOutDestroysAuthentication(t *testing.T) {
	manager := NewSessionManager(memstore.New(), time.Hour, false)
	userID := uuid.New()

	login := httptest.NewRecorder()
	manager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := SignInSession(r.Context(), manager, userID); err != nil {
			t.Fatalf("SignInSession() error = %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/", nil))

	logout := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.AddCookie(login.Result().Cookies()[0])
	manager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := SignOutSession(r.Context(), manager); err != nil {
			t.Fatalf("SignOutSession() error = %v", err)
		}
		if _, err := CurrentUserID(r.Context(), manager); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("CurrentUserID() error = %v, want ErrUnauthenticated", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(logout, request)

	cleared := logout.Result().Cookies()[0]
	if cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Fatalf("cleared cookie = value %q, max-age %d", cleared.Value, cleared.MaxAge)
	}
}
