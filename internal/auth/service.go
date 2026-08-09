package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidInput       = errors.New("invalid input")
	ErrUserNotFound       = errors.New("user not found")
)

type User struct {
	ID        uuid.UUID
	Email     string
	CreatedAt time.Time
}

type Credentials struct {
	User
	PasswordHash string
}

type UserRepository interface {
	Create(ctx context.Context, email, passwordHash string) (User, error)
	ByEmail(ctx context.Context, email string) (Credentials, error)
	ByID(ctx context.Context, id uuid.UUID) (User, error)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (bool, error)
}

type Service struct {
	users  UserRepository
	hasher PasswordHasher
}

func NewService(users UserRepository, hasher PasswordHasher) *Service {
	return &Service{users: users, hasher: hasher}
}

func (s *Service) Register(ctx context.Context, email, password string) (User, error) {
	email = normalizeEmail(email)
	if !validEmail(email) || !validPassword(password) {
		return User{}, ErrInvalidInput
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return User{}, err
	}
	return s.users.Create(ctx, email, hash)
}

func (s *Service) Authenticate(ctx context.Context, email, password string) (User, error) {
	credentials, err := s.users.ByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return User{}, ErrInvalidCredentials
		}
		return User{}, err
	}
	match, err := s.hasher.Verify(password, credentials.PasswordHash)
	if err != nil {
		return User{}, err
	}
	if !match {
		return User{}, ErrInvalidCredentials
	}
	return credentials.User, nil
}

func (s *Service) User(ctx context.Context, id uuid.UUID) (User, error) {
	return s.users.ByID(ctx, id)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validEmail(email string) bool {
	if email == "" || len(email) > 320 {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Name == "" && address.Address == email
}

func validPassword(password string) bool {
	length := utf8.RuneCountInString(password)
	return length >= 12 && length <= 128
}
