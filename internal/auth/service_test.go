package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryUsers struct {
	byEmail map[string]Credentials
}

func newMemoryUsers() *memoryUsers {
	return &memoryUsers{byEmail: make(map[string]Credentials)}
}

func (m *memoryUsers) Create(_ context.Context, email, passwordHash string) (User, error) {
	if _, exists := m.byEmail[email]; exists {
		return User{}, ErrEmailTaken
	}
	user := User{ID: uuid.New(), Email: email, CreatedAt: time.Now()}
	m.byEmail[email] = Credentials{User: user, PasswordHash: passwordHash}
	return user, nil
}

func (m *memoryUsers) ByEmail(_ context.Context, email string) (Credentials, error) {
	credentials, exists := m.byEmail[email]
	if !exists {
		return Credentials{}, ErrUserNotFound
	}
	return credentials, nil
}

func (m *memoryUsers) ByID(_ context.Context, id uuid.UUID) (User, error) {
	for _, credentials := range m.byEmail {
		if credentials.ID == id {
			return credentials.User, nil
		}
	}
	return User{}, ErrUserNotFound
}

type deterministicHasher struct{}

func (deterministicHasher) Hash(password string) (string, error) {
	return "hashed:" + password, nil
}

func (deterministicHasher) Verify(password, encodedHash string) (bool, error) {
	return encodedHash == "hashed:"+password, nil
}

func TestRegisterNormalizesEmailAndStoresOnlyPasswordHash(t *testing.T) {
	repository := newMemoryUsers()
	service := NewService(repository, deterministicHasher{})

	user, err := service.Register(context.Background(), "  Person@Example.COM ", "correct horse battery")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if user.Email != "person@example.com" {
		t.Fatalf("user email = %q, want normalized email", user.Email)
	}
	if got := repository.byEmail[user.Email].PasswordHash; got != "hashed:correct horse battery" {
		t.Fatalf("stored password = %q, want encoded hash", got)
	}
}

func TestRegisterRejectsInvalidInputAndDuplicateEmail(t *testing.T) {
	service := NewService(newMemoryUsers(), deterministicHasher{})

	for name, input := range map[string]struct {
		email    string
		password string
	}{
		"invalid email":  {email: "not-an-email", password: "correct horse battery"},
		"short password": {email: "person@example.com", password: "too-short"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.Register(context.Background(), input.email, input.password)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Register() error = %v, want ErrInvalidInput", err)
			}
		})
	}

	if _, err := service.Register(context.Background(), "person@example.com", "correct horse battery"); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if _, err := service.Register(context.Background(), "PERSON@example.com", "another correct password"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate Register() error = %v, want ErrEmailTaken", err)
	}
}

func TestAuthenticateReturnsGenericErrorForUnknownEmailAndWrongPassword(t *testing.T) {
	repository := newMemoryUsers()
	service := NewService(repository, deterministicHasher{})
	if _, err := service.Register(context.Background(), "person@example.com", "correct horse battery"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	for name, input := range map[string][2]string{
		"unknown email":  {"missing@example.com", "correct horse battery"},
		"wrong password": {"person@example.com", "wrong horse battery"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.Authenticate(context.Background(), input[0], input[1])
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Authenticate() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestAuthenticateReturnsUserForValidCredentials(t *testing.T) {
	service := NewService(newMemoryUsers(), deterministicHasher{})
	want, err := service.Register(context.Background(), "person@example.com", "correct horse battery")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, err := service.Authenticate(context.Background(), "PERSON@example.com", "correct horse battery")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if got.ID != want.ID {
		t.Fatalf("Authenticate() user ID = %v, want %v", got.ID, want.ID)
	}
}
