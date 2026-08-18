package auth

import (
	"context"
	"fmt"

	"github.com/Godspower-Eze/infoai-backend/ent"
	entuser "github.com/Godspower-Eze/infoai-backend/ent/user"
	"github.com/google/uuid"
)

type EntUserRepository struct {
	client *ent.Client
}

func NewEntUserRepository(client *ent.Client) *EntUserRepository {
	return &EntUserRepository{client: client}
}

func (r *EntUserRepository) Create(ctx context.Context, email, passwordHash string) (User, error) {
	stored, err := r.client.User.Create().
		SetEmail(email).
		SetPasswordHash(passwordHash).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return User{}, ErrEmailTaken
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return userFromEnt(stored), nil
}

func (r *EntUserRepository) ByEmail(ctx context.Context, email string) (Credentials, error) {
	stored, err := r.client.User.Query().Where(entuser.EmailEQ(email)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return Credentials{}, ErrUserNotFound
		}
		return Credentials{}, fmt.Errorf("find user by email: %w", err)
	}
	return Credentials{User: userFromEnt(stored), PasswordHash: stored.PasswordHash}, nil
}

func (r *EntUserRepository) ByID(ctx context.Context, id uuid.UUID) (User, error) {
	stored, err := r.client.User.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("find user by id: %w", err)
	}
	return userFromEnt(stored), nil
}

func userFromEnt(stored *ent.User) User {
	return User{
		ID:        stored.ID,
		Email:     stored.Email,
		CreatedAt: stored.CreatedAt,
	}
}

var _ UserRepository = (*EntUserRepository)(nil)
