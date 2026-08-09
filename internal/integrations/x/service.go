package xintegration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const refreshWindow = 5 * time.Minute

var (
	ErrAccountNotFound       = errors.New("X account not found")
	ErrAccountOwned          = errors.New("X account is connected to another user")
	ErrReauthorizationNeeded = errors.New("X account must be reauthorized")
)

type OAuthToken struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	Scopes       []string
}

type Profile struct {
	ID              string
	Username        string
	DisplayName     string
	ProfileImageURL *string
}

type Account struct {
	ID              uuid.UUID
	OwnerID         uuid.UUID
	XUserID         string
	Username        string
	DisplayName     string
	ProfileImageURL *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type EncryptedGrant struct {
	AccessToken  []byte
	RefreshToken []byte
	Expiry       time.Time
	Scopes       []string
}

type StoredGrant struct {
	Account
	EncryptedGrant
}

type Provider interface {
	AuthorizationURL(state, challenge string) string
	Exchange(ctx context.Context, code, verifier string) (OAuthToken, error)
	CurrentUser(ctx context.Context, accessToken string) (Profile, error)
	Refresh(ctx context.Context, refreshToken string) (OAuthToken, error)
	Revoke(ctx context.Context, token string) error
}

type AccountRepository interface {
	Upsert(ctx context.Context, ownerID uuid.UUID, profile Profile, grant EncryptedGrant) (Account, error)
	List(ctx context.Context, ownerID uuid.UUID) ([]Account, error)
	Grant(ctx context.Context, ownerID, accountID uuid.UUID) (StoredGrant, error)
	UpdateGrant(ctx context.Context, ownerID, accountID uuid.UUID, grant EncryptedGrant) error
	Delete(ctx context.Context, ownerID, accountID uuid.UUID) (StoredGrant, error)
}

type Encryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type Service struct {
	provider  Provider
	accounts  AccountRepository
	encryptor Encryptor
	oauth     *OAuthSession
	now       func() time.Time
}

func NewService(provider Provider, accounts AccountRepository, encryptor Encryptor, oauth *OAuthSession, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{provider: provider, accounts: accounts, encryptor: encryptor, oauth: oauth, now: now}
}

func (s *Service) BeginAuthorization(ctx context.Context) (string, error) {
	return s.oauth.Begin(ctx, s.provider.AuthorizationURL)
}

func (s *Service) CompleteAuthorization(ctx context.Context, ownerID uuid.UUID, state, code string) (Account, error) {
	verifier, err := s.oauth.Consume(ctx, state)
	if err != nil {
		return Account{}, err
	}
	token, err := s.provider.Exchange(ctx, code, verifier)
	if err != nil {
		return Account{}, fmt.Errorf("exchange X authorization code: %w", err)
	}
	profile, err := s.provider.CurrentUser(ctx, token.AccessToken)
	if err != nil {
		return Account{}, fmt.Errorf("fetch X profile: %w", err)
	}
	grant, err := s.encryptGrant(token)
	if err != nil {
		return Account{}, err
	}
	return s.accounts.Upsert(ctx, ownerID, profile, grant)
}

func (s *Service) Accounts(ctx context.Context, ownerID uuid.UUID) ([]Account, error) {
	return s.accounts.List(ctx, ownerID)
}

func (s *Service) AccessToken(ctx context.Context, ownerID, accountID uuid.UUID) (string, error) {
	stored, err := s.accounts.Grant(ctx, ownerID, accountID)
	if err != nil {
		return "", err
	}
	if stored.Expiry.After(s.now().Add(refreshWindow)) {
		return s.decryptString(stored.AccessToken)
	}
	if len(stored.RefreshToken) == 0 {
		return "", ErrReauthorizationNeeded
	}
	refreshToken, err := s.decryptString(stored.RefreshToken)
	if err != nil {
		return "", err
	}
	refreshed, err := s.provider.Refresh(ctx, refreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh X access token: %w", err)
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = refreshToken
	}
	if len(refreshed.Scopes) == 0 {
		refreshed.Scopes = stored.Scopes
	}
	grant, err := s.encryptGrant(refreshed)
	if err != nil {
		return "", err
	}
	if err := s.accounts.UpdateGrant(ctx, ownerID, accountID, grant); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (s *Service) Disconnect(ctx context.Context, ownerID, accountID uuid.UUID) error {
	stored, err := s.accounts.Delete(ctx, ownerID, accountID)
	if err != nil {
		return err
	}
	ciphertext := stored.RefreshToken
	if len(ciphertext) == 0 {
		ciphertext = stored.AccessToken
	}
	token, err := s.decryptString(ciphertext)
	if err == nil {
		_ = s.provider.Revoke(ctx, token)
	}
	return nil
}

func (s *Service) encryptGrant(token OAuthToken) (EncryptedGrant, error) {
	access, err := s.encryptor.Encrypt([]byte(token.AccessToken))
	if err != nil {
		return EncryptedGrant{}, fmt.Errorf("encrypt X access token: %w", err)
	}
	var refresh []byte
	if token.RefreshToken != "" {
		refresh, err = s.encryptor.Encrypt([]byte(token.RefreshToken))
		if err != nil {
			return EncryptedGrant{}, fmt.Errorf("encrypt X refresh token: %w", err)
		}
	}
	return EncryptedGrant{AccessToken: access, RefreshToken: refresh, Expiry: token.Expiry, Scopes: token.Scopes}, nil
}

func (s *Service) decryptString(ciphertext []byte) (string, error) {
	plaintext, err := s.encryptor.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt X token: %w", err)
	}
	return string(plaintext), nil
}
