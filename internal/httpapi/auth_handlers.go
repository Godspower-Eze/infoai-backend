package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/godspowere/infoai-backend/internal/auth"
)

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func (api *API) signup(response http.ResponseWriter, request *http.Request) {
	var input credentialsRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusUnprocessableEntity, "validation_error", "The request contains invalid fields.", nil)
		return
	}
	user, err := api.auth.Register(request.Context(), input.Email, input.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidInput):
			writeError(response, http.StatusUnprocessableEntity, "validation_error", "The request contains invalid fields.", nil)
		case errors.Is(err, auth.ErrEmailTaken):
			writeError(response, http.StatusConflict, "email_taken", "An account with this email already exists.", nil)
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
		}
		return
	}
	if err := auth.SignInSession(request.Context(), api.sessions, user.ID); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
		return
	}
	writeUser(response, http.StatusCreated, user)
}

func (api *API) signin(response http.ResponseWriter, request *http.Request) {
	var input credentialsRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusUnprocessableEntity, "validation_error", "The request contains invalid fields.", nil)
		return
	}
	if !api.signinEmails.Allow(strings.ToLower(strings.TrimSpace(input.Email)), time.Now()) {
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again later.", nil)
		return
	}
	user, err := api.auth.Authenticate(request.Context(), input.Email, input.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(response, http.StatusUnauthorized, "invalid_credentials", "The supplied credentials are invalid.", nil)
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
		return
	}
	if err := auth.SignInSession(request.Context(), api.sessions, user.ID); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
		return
	}
	writeUser(response, http.StatusOK, user)
}

func (api *API) signout(response http.ResponseWriter, request *http.Request) {
	if err := auth.SignOutSession(request.Context(), api.sessions); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) me(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.User(request.Context(), requestUserID(request))
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			_ = auth.SignOutSession(request.Context(), api.sessions)
			writeError(response, http.StatusUnauthorized, "unauthenticated", "Authentication is required.", nil)
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
		return
	}
	writeUser(response, http.StatusOK, user)
}

func writeUser(response http.ResponseWriter, status int, user auth.User) {
	writeJSON(response, status, map[string]any{"data": map[string]any{"user": userResponse{ID: user.ID.String(), Email: user.Email, CreatedAt: user.CreatedAt}}})
}
