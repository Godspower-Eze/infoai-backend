package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
)

const sessionUserIDKey = "user_id"

var ErrUnauthenticated = errors.New("authentication required")

func NewSessionManager(store scs.Store, lifetime time.Duration, secure bool) *scs.SessionManager {
	manager := scs.New()
	manager.Store = store
	manager.Lifetime = lifetime
	manager.HashTokenInStore = true
	manager.Cookie.Name = "infoai_session"
	manager.Cookie.Path = "/"
	manager.Cookie.HttpOnly = true
	manager.Cookie.SameSite = http.SameSiteLaxMode
	manager.Cookie.Secure = secure
	manager.Cookie.Persist = true
	return manager
}

func SignInSession(ctx context.Context, manager *scs.SessionManager, userID uuid.UUID) error {
	if err := manager.RenewToken(ctx); err != nil {
		return err
	}
	manager.Put(ctx, sessionUserIDKey, userID.String())
	return nil
}

func SignOutSession(ctx context.Context, manager *scs.SessionManager) error {
	return manager.Destroy(ctx)
}

func CurrentUserID(ctx context.Context, manager *scs.SessionManager) (uuid.UUID, error) {
	raw := manager.GetString(ctx, sessionUserIDKey)
	if raw == "" {
		return uuid.Nil, ErrUnauthenticated
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, ErrUnauthenticated
	}
	return id, nil
}
