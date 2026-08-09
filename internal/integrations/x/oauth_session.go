package xintegration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/alexedwards/scs/v2"
	"golang.org/x/oauth2"
)

const (
	oauthStateHashKey = "x_oauth_state_hash"
	oauthVerifierKey  = "x_oauth_pkce_verifier"
	oauthExpiryKey    = "x_oauth_expiry"
	oauthAttemptTTL   = 10 * time.Minute
)

var ErrInvalidOAuthState = errors.New("invalid or expired OAuth state")

type OAuthSession struct {
	sessions *scs.SessionManager
	now      func() time.Time
}

func NewOAuthSession(sessions *scs.SessionManager, now func() time.Time) *OAuthSession {
	if now == nil {
		now = time.Now
	}
	return &OAuthSession{sessions: sessions, now: now}
}

func (s *OAuthSession) Begin(ctx context.Context, authorizationURL func(state, challenge string) string) (string, error) {
	state, err := randomState()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	s.sessions.Put(ctx, oauthStateHashKey, hashState(state))
	s.sessions.Put(ctx, oauthVerifierKey, verifier)
	s.sessions.Put(ctx, oauthExpiryKey, s.now().Add(oauthAttemptTTL).Unix())
	return authorizationURL(state, PKCEChallenge(verifier)), nil
}

func (s *OAuthSession) Consume(ctx context.Context, state string) (string, error) {
	wantHash := s.sessions.GetString(ctx, oauthStateHashKey)
	verifier := s.sessions.GetString(ctx, oauthVerifierKey)
	expiry := s.sessions.GetInt64(ctx, oauthExpiryKey)
	s.clear(ctx)

	gotHash := hashState(state)
	validHash := subtle.ConstantTimeCompare([]byte(gotHash), []byte(wantHash)) == 1
	if wantHash == "" || verifier == "" || !validHash || expiry <= s.now().Unix() {
		return "", ErrInvalidOAuthState
	}
	return verifier, nil
}

func (s *OAuthSession) clear(ctx context.Context) {
	s.sessions.Remove(ctx, oauthStateHashKey)
	s.sessions.Remove(ctx, oauthVerifierKey)
	s.sessions.Remove(ctx, oauthExpiryKey)
}

func PKCEChallenge(verifier string) string {
	return oauth2.S256ChallengeFromVerifier(verifier)
}

func randomState() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashState(state string) string {
	hash := sha256.Sum256([]byte(state))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
