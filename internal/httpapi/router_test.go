package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/auth"
	xintegration "github.com/Godspower-Eze/infoai-backend/internal/integrations/x"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/google/uuid"
)

const testFrontendOrigin = "http://localhost:3000"

type stubAuth struct {
	user            auth.User
	registerErr     error
	authenticateErr error
	userErr         error
	registeredEmail string
}

func (s *stubAuth) Register(_ context.Context, email, _ string) (auth.User, error) {
	s.registeredEmail = email
	return s.user, s.registerErr
}

func (s *stubAuth) Authenticate(context.Context, string, string) (auth.User, error) {
	return s.user, s.authenticateErr
}

func (s *stubAuth) User(context.Context, uuid.UUID) (auth.User, error) {
	return s.user, s.userErr
}

type stubX struct {
	accounts       []xintegration.Account
	beginURL       string
	complete       xintegration.Account
	completeErr    error
	disconnectedID uuid.UUID
}

func (s *stubX) BeginAuthorization(context.Context) (string, error) {
	return s.beginURL, nil
}

func (s *stubX) CompleteAuthorization(context.Context, uuid.UUID, string, string) (xintegration.Account, error) {
	return s.complete, s.completeErr
}

func (s *stubX) Accounts(context.Context, uuid.UUID) ([]xintegration.Account, error) {
	return s.accounts, nil
}

func (s *stubX) Disconnect(_ context.Context, _, accountID uuid.UUID) error {
	s.disconnectedID = accountID
	return nil
}

type stubReadiness struct{ err error }

func (s stubReadiness) Ping(context.Context) error { return s.err }

type testAPI struct {
	handler http.Handler
	auth    *stubAuth
	x       *stubX
}

func newTestAPI(t *testing.T) testAPI {
	return newTestAPIWithProxies(t, nil)
}

func newTestAPIWithProxies(t *testing.T, trustedProxyCIDRs []string) testAPI {
	t.Helper()
	user := auth.User{ID: uuid.New(), Email: "person@example.com", CreatedAt: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)}
	authService := &stubAuth{user: user}
	xService := &stubX{beginURL: "https://x.example/authorize"}
	sessions := auth.NewSessionManager(memstore.New(), time.Hour, false)
	handler, err := NewRouter(Dependencies{
		Auth:                 authService,
		X:                    xService,
		Sessions:             sessions,
		Readiness:            stubReadiness{},
		FrontendOrigin:       testFrontendOrigin,
		FrontendXRedirectURL: testFrontendOrigin + "/settings/integrations",
		TrustedProxyCIDRs:    trustedProxyCIDRs,
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return testAPI{handler: handler, auth: authService, x: xService}
}

func TestSignupRateLimitUsesClientIPResolvedThroughTrustedProxy(t *testing.T) {
	api := newTestAPIWithProxies(t, []string{"10.0.0.0/8"})

	for attempt := 0; attempt < 6; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", bytes.NewBufferString(`{"email":"person@example.com","password":"correct horse battery"}`))
		req.RemoteAddr = "10.0.0.10:443"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", testFrontendOrigin)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d, 10.0.0.10", attempt+1))
		response := httptest.NewRecorder()
		api.handler.ServeHTTP(response, req)
		if response.Code != http.StatusCreated {
			t.Fatalf("client %d status = %d, body = %s", attempt+1, response.Code, response.Body.String())
		}
	}
}

func request(t *testing.T, handler http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &payload)
	req.RemoteAddr = "192.0.2.10:1234"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("Origin", testFrontendOrigin)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func signup(t *testing.T, api testAPI) *http.Cookie {
	t.Helper()
	response := request(t, api.handler, http.MethodPost, "/api/v1/auth/signup", map[string]string{"email": "person@example.com", "password": "correct horse battery"}, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("signup cookies = %d, want 1", len(cookies))
	}
	return cookies[0]
}

func TestAuthenticationRoutesCreateUseAndDestroySession(t *testing.T) {
	api := newTestAPI(t)
	cookie := signup(t, api)
	if cookie.Name != "infoai_session" || !cookie.HttpOnly {
		t.Fatalf("signup cookie = %+v", cookie)
	}
	if api.auth.registeredEmail != "person@example.com" {
		t.Fatalf("Register() email = %q", api.auth.registeredEmail)
	}

	me := request(t, api.handler, http.MethodGet, "/api/v1/auth/me", nil, cookie)
	if me.Code != http.StatusOK || bytes.Contains(me.Body.Bytes(), []byte("password")) {
		t.Fatalf("me status = %d, body = %s", me.Code, me.Body.String())
	}

	logout := request(t, api.handler, http.MethodPost, "/api/v1/auth/signout", nil, cookie)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("signout status = %d, body = %s", logout.Code, logout.Body.String())
	}
	cleared := logout.Result().Cookies()[0]
	if cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Fatalf("signout cookie = %+v", cleared)
	}
}

func TestAuthenticationErrorsUseStableEnvelope(t *testing.T) {
	api := newTestAPI(t)
	api.auth.authenticateErr = auth.ErrInvalidCredentials

	response := request(t, api.handler, http.MethodPost, "/api/v1/auth/signin", map[string]string{"email": "person@example.com", "password": "incorrect password"}, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("signin status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Error.Code != "invalid_credentials" || payload.Error.Message != "The supplied credentials are invalid." {
		t.Fatalf("error = %+v", payload.Error)
	}
}

func TestSigninRateLimitsNormalizedEmail(t *testing.T) {
	api := newTestAPI(t)
	api.auth.authenticateErr = auth.ErrInvalidCredentials

	for attempt := 0; attempt < 5; attempt++ {
		response := request(t, api.handler, http.MethodPost, "/api/v1/auth/signin", map[string]string{"email": " Person@Example.COM ", "password": "incorrect password"}, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, body = %s", attempt+1, response.Code, response.Body.String())
		}
	}
	response := request(t, api.handler, http.MethodPost, "/api/v1/auth/signin", map[string]string{"email": "person@example.com", "password": "incorrect password"}, nil)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth normalized-email attempt status = %d, want 429", response.Code)
	}
}

func TestRouterRejectsUntrustedUnsafeOrigin(t *testing.T) {
	api := newTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", bytes.NewBufferString(`{"email":"person@example.com","password":"correct horse battery"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("untrusted origin status = %d, want 403", response.Code)
	}
}

func TestXRoutesRequireAuthenticationAndExposeOnlyMetadata(t *testing.T) {
	api := newTestAPI(t)
	unauthenticated := request(t, api.handler, http.MethodGet, "/api/v1/integrations/x/accounts", nil, nil)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}
	cookie := signup(t, api)
	accountID := uuid.New()
	checkedAt := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	api.x.accounts = []xintegration.Account{{ID: accountID, XUserID: "x-123", Username: "person", DisplayName: "A Person", SubscriptionType: "PremiumPlus", SubscriptionCheckedAt: &checkedAt}}

	authorize := request(t, api.handler, http.MethodPost, "/api/v1/integrations/x/authorize", nil, cookie)
	if authorize.Code != http.StatusOK || !bytes.Contains(authorize.Body.Bytes(), []byte("https://x.example/authorize")) {
		t.Fatalf("authorize status = %d, body = %s", authorize.Code, authorize.Body.String())
	}
	accounts := request(t, api.handler, http.MethodGet, "/api/v1/integrations/x/accounts", nil, cookie)
	if accounts.Code != http.StatusOK || bytes.Contains(accounts.Body.Bytes(), []byte("token")) {
		t.Fatalf("accounts status = %d, body = %s", accounts.Code, accounts.Body.String())
	}
	if !bytes.Contains(accounts.Body.Bytes(), []byte(`"subscription_type":"PremiumPlus"`)) || !bytes.Contains(accounts.Body.Bytes(), []byte(`"subscription_checked_at":"2026-08-18T12:00:00Z"`)) {
		t.Fatalf("accounts response omits subscription metadata: %s", accounts.Body.String())
	}
	disconnect := request(t, api.handler, http.MethodDelete, "/api/v1/integrations/x/accounts/"+accountID.String(), nil, cookie)
	if disconnect.Code != http.StatusNoContent || api.x.disconnectedID != accountID {
		t.Fatalf("disconnect status = %d, disconnected ID = %v", disconnect.Code, api.x.disconnectedID)
	}
}

func TestXCallbackRedirectsWithStableSuccessAndErrorResults(t *testing.T) {
	api := newTestAPI(t)
	cookie := signup(t, api)
	accountID := uuid.New()
	api.x.complete = xintegration.Account{ID: accountID}

	success := request(t, api.handler, http.MethodGet, "/api/v1/integrations/x/callback?state=state&code=code", nil, cookie)
	if success.Code != http.StatusSeeOther {
		t.Fatalf("callback success status = %d", success.Code)
	}
	location, _ := url.Parse(success.Header().Get("Location"))
	if location.Query().Get("x_connection") != "success" || location.Query().Get("account_id") != accountID.String() {
		t.Fatalf("callback success location = %q", location.String())
	}

	api.x.completeErr = xintegration.ErrAccountOwned
	failure := request(t, api.handler, http.MethodGet, "/api/v1/integrations/x/callback?state=state&code=code", nil, cookie)
	location, _ = url.Parse(failure.Header().Get("Location"))
	if failure.Code != http.StatusSeeOther || location.Query().Get("x_connection") != "error" || location.Query().Get("code") != "x_account_owned" {
		t.Fatalf("callback failure status = %d, location = %q", failure.Code, location.String())
	}
}

func TestHealthRoutesDistinguishLivenessAndReadiness(t *testing.T) {
	api := newTestAPI(t)
	live := request(t, api.handler, http.MethodGet, "/health/live", nil, nil)
	ready := request(t, api.handler, http.MethodGet, "/health/ready", nil, nil)
	if live.Code != http.StatusOK || ready.Code != http.StatusOK {
		t.Fatalf("healthy statuses = live %d, ready %d", live.Code, ready.Code)
	}

	sessions := auth.NewSessionManager(memstore.New(), time.Hour, false)
	handler, err := NewRouter(Dependencies{Auth: api.auth, X: api.x, Sessions: sessions, Readiness: stubReadiness{err: errors.New("database unavailable")}, FrontendOrigin: testFrontendOrigin, FrontendXRedirectURL: testFrontendOrigin + "/settings/integrations"})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	notReady := request(t, handler, http.MethodGet, "/health/ready", nil, nil)
	if notReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("unready status = %d, want 503", notReady.Code)
	}
}
