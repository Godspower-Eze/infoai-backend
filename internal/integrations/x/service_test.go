package xintegration

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/google/uuid"
)

func queryValue(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	return parsed.Query().Get(key)
}

type fakeProvider struct {
	exchangedToken OAuthToken
	profile        Profile
	refreshedToken OAuthToken
	exchangeCode   string
	exchangePKCE   string
	refreshInput   string
	revokeInput    string
	revokeErr      error
}

func (f *fakeProvider) AuthorizationURL(state, challenge string) string {
	return "https://x.example/authorize?state=" + state + "&challenge=" + challenge
}

func (f *fakeProvider) Exchange(_ context.Context, code, verifier string) (OAuthToken, error) {
	f.exchangeCode, f.exchangePKCE = code, verifier
	return f.exchangedToken, nil
}

func (f *fakeProvider) CurrentUser(context.Context, string) (Profile, error) {
	return f.profile, nil
}

func (f *fakeProvider) Refresh(_ context.Context, refreshToken string) (OAuthToken, error) {
	f.refreshInput = refreshToken
	return f.refreshedToken, nil
}

func (f *fakeProvider) Revoke(_ context.Context, token string) error {
	f.revokeInput = token
	return f.revokeErr
}

type memoryAccounts struct {
	accounts map[uuid.UUID]StoredGrant
}

func newMemoryAccounts() *memoryAccounts {
	return &memoryAccounts{accounts: make(map[uuid.UUID]StoredGrant)}
}

func (m *memoryAccounts) Upsert(_ context.Context, ownerID uuid.UUID, profile Profile, grant EncryptedGrant) (Account, error) {
	for id, existing := range m.accounts {
		if existing.XUserID != profile.ID {
			continue
		}
		if existing.OwnerID != ownerID {
			return Account{}, ErrAccountOwned
		}
		existing.Username = profile.Username
		existing.DisplayName = profile.DisplayName
		existing.ProfileImageURL = profile.ProfileImageURL
		existing.SubscriptionType = profile.SubscriptionType
		existing.SubscriptionCheckedAt = profile.SubscriptionCheckedAt
		existing.EncryptedGrant = grant
		m.accounts[id] = existing
		return existing.Account, nil
	}
	account := Account{ID: uuid.New(), OwnerID: ownerID, XUserID: profile.ID, Username: profile.Username, DisplayName: profile.DisplayName, ProfileImageURL: profile.ProfileImageURL, SubscriptionType: profile.SubscriptionType, SubscriptionCheckedAt: profile.SubscriptionCheckedAt}
	m.accounts[account.ID] = StoredGrant{Account: account, EncryptedGrant: grant}
	return account, nil
}

func (m *memoryAccounts) List(_ context.Context, ownerID uuid.UUID) ([]Account, error) {
	var result []Account
	for _, account := range m.accounts {
		if account.OwnerID == ownerID {
			result = append(result, account.Account)
		}
	}
	return result, nil
}

func (m *memoryAccounts) Grant(_ context.Context, ownerID, accountID uuid.UUID) (StoredGrant, error) {
	account, exists := m.accounts[accountID]
	if !exists || account.OwnerID != ownerID {
		return StoredGrant{}, ErrAccountNotFound
	}
	return account, nil
}

func (m *memoryAccounts) UpdateGrant(_ context.Context, ownerID, accountID uuid.UUID, grant EncryptedGrant) error {
	account, err := m.Grant(context.Background(), ownerID, accountID)
	if err != nil {
		return err
	}
	account.EncryptedGrant = grant
	m.accounts[accountID] = account
	return nil
}

func (m *memoryAccounts) Delete(_ context.Context, ownerID, accountID uuid.UUID) (StoredGrant, error) {
	account, err := m.Grant(context.Background(), ownerID, accountID)
	if err != nil {
		return StoredGrant{}, err
	}
	delete(m.accounts, accountID)
	return account, nil
}

func testService(t *testing.T, now time.Time, provider *fakeProvider, repository *memoryAccounts) (*Service, *scs.SessionManager) {
	t.Helper()
	manager := scs.New()
	manager.Store = memstore.New()
	encryptor, err := NewAESGCMEncryptor(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor() error = %v", err)
	}
	flow := NewOAuthSession(manager, func() time.Time { return now })
	return NewService(provider, repository, encryptor, flow, func() time.Time { return now }), manager
}

func TestCompleteAuthorizationStoresEncryptedExclusiveGrant(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	provider := &fakeProvider{
		exchangedToken: OAuthToken{AccessToken: "access-secret", RefreshToken: "refresh-secret", Expiry: now.Add(2 * time.Hour), Scopes: []string{"tweet.read", "tweet.write"}},
		profile:        Profile{ID: "x-123", Username: "person", DisplayName: "A Person", SubscriptionType: "Premium"},
	}
	repository := newMemoryAccounts()
	service, sessions := testService(t, now, provider, repository)
	ownerID := uuid.New()

	var state string
	_, cookie := withSession(t, sessions, nil, func(w http.ResponseWriter, r *http.Request) {
		url, err := service.BeginAuthorization(r.Context())
		if err != nil {
			t.Fatalf("BeginAuthorization() error = %v", err)
		}
		state = sessions.GetString(r.Context(), oauthStateHashKey)
		if url == "" || state == "" {
			t.Fatal("BeginAuthorization() did not persist state or return URL")
		}
	})

	withSession(t, sessions, cookie, func(w http.ResponseWriter, r *http.Request) {
		// The callback receives the original state, so start another attempt and capture it from the provider URL.
		url, err := service.BeginAuthorization(r.Context())
		if err != nil {
			t.Fatalf("second BeginAuthorization() error = %v", err)
		}
		state = queryValue(t, url, "state")
		account, err := service.CompleteAuthorization(r.Context(), ownerID, state, "authorization-code")
		if err != nil {
			t.Fatalf("CompleteAuthorization() error = %v", err)
		}
		stored := repository.accounts[account.ID]
		if stored.SubscriptionType != "Premium" || stored.SubscriptionCheckedAt == nil || !stored.SubscriptionCheckedAt.Equal(now) {
			t.Fatalf("stored subscription metadata = %q/%v, want Premium/%v", stored.SubscriptionType, stored.SubscriptionCheckedAt, now)
		}
		if bytes.Contains(stored.AccessToken, []byte("access-secret")) || bytes.Contains(stored.RefreshToken, []byte("refresh-secret")) {
			t.Fatal("stored grant contains plaintext OAuth token")
		}
		if provider.exchangeCode != "authorization-code" || provider.exchangePKCE == "" {
			t.Fatalf("Exchange() inputs = code %q verifier %q", provider.exchangeCode, provider.exchangePKCE)
		}
	})
}

func TestAccessTokenRefreshesNearExpiryAndPersistsRotation(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	provider := &fakeProvider{refreshedToken: OAuthToken{AccessToken: "new-access", RefreshToken: "new-refresh", Expiry: now.Add(2 * time.Hour), Scopes: []string{"tweet.read"}}}
	repository := newMemoryAccounts()
	service, _ := testService(t, now, provider, repository)
	ownerID, accountID := uuid.New(), uuid.New()
	access, _ := service.encryptor.Encrypt([]byte("old-access"))
	refresh, _ := service.encryptor.Encrypt([]byte("old-refresh"))
	repository.accounts[accountID] = StoredGrant{Account: Account{ID: accountID, OwnerID: ownerID}, EncryptedGrant: EncryptedGrant{AccessToken: access, RefreshToken: refresh, Expiry: now.Add(4 * time.Minute)}}

	token, err := service.AccessToken(context.Background(), ownerID, accountID)
	if err != nil {
		t.Fatalf("AccessToken() error = %v", err)
	}
	if token != "new-access" || provider.refreshInput != "old-refresh" {
		t.Fatalf("AccessToken() = %q, refresh input = %q", token, provider.refreshInput)
	}
	stored := repository.accounts[accountID]
	decrypted, err := service.encryptor.Decrypt(stored.AccessToken)
	if err != nil || string(decrypted) != "new-access" {
		t.Fatalf("persisted access token = %q, error = %v", decrypted, err)
	}
}

func TestDisconnectDeletesLocallyWhenRemoteRevocationFails(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	provider := &fakeProvider{revokeErr: errors.New("X unavailable")}
	repository := newMemoryAccounts()
	service, _ := testService(t, now, provider, repository)
	ownerID, accountID := uuid.New(), uuid.New()
	access, _ := service.encryptor.Encrypt([]byte("access"))
	refresh, _ := service.encryptor.Encrypt([]byte("refresh"))
	repository.accounts[accountID] = StoredGrant{Account: Account{ID: accountID, OwnerID: ownerID}, EncryptedGrant: EncryptedGrant{AccessToken: access, RefreshToken: refresh, Expiry: now.Add(time.Hour)}}

	if err := service.Disconnect(context.Background(), ownerID, accountID); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if _, exists := repository.accounts[accountID]; exists {
		t.Fatal("Disconnect() retained local credentials")
	}
	if provider.revokeInput != "refresh" {
		t.Fatalf("Revoke() input = %q, want refresh token", provider.revokeInput)
	}
}
