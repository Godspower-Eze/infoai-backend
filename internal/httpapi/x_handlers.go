package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	xintegration "github.com/Godspower-Eze/infoai-backend/internal/integrations/x"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (api *API) authorizeX(response http.ResponseWriter, request *http.Request) {
	authorizationURL, err := api.x.BeginAuthorization(request.Context())
	if err != nil {
		writeError(response, http.StatusBadGateway, "x_authorization_failed", "X authorization could not be started.", nil)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": map[string]string{"authorization_url": authorizationURL}})
}

func (api *API) xCallback(response http.ResponseWriter, request *http.Request) {
	if request.URL.Query().Get("error") != "" {
		api.redirectXResult(response, request, "", "x_authorization_denied")
		return
	}
	state, code := request.URL.Query().Get("state"), request.URL.Query().Get("code")
	if state == "" || code == "" {
		api.redirectXResult(response, request, "", "invalid_x_callback")
		return
	}
	account, err := api.x.CompleteAuthorization(request.Context(), requestUserID(request), state, code)
	if err != nil {
		resultCode := "x_connection_failed"
		switch {
		case errors.Is(err, xintegration.ErrInvalidOAuthState):
			resultCode = "invalid_oauth_state"
		case errors.Is(err, xintegration.ErrAccountOwned):
			resultCode = "x_account_owned"
		}
		api.redirectXResult(response, request, "", resultCode)
		return
	}
	api.redirectXResult(response, request, account.ID.String(), "")
}

func (api *API) xAccounts(response http.ResponseWriter, request *http.Request) {
	accounts, err := api.x.Accounts(request.Context(), requestUserID(request))
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
		return
	}
	publicAccounts := make([]xAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		publicAccounts = append(publicAccounts, newXAccountResponse(account))
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": map[string]any{"accounts": publicAccounts}})
}

type xAccountResponse struct {
	ID                    uuid.UUID  `json:"id"`
	XUserID               string     `json:"x_user_id"`
	Username              string     `json:"username"`
	DisplayName           string     `json:"display_name"`
	ProfileImageURL       *string    `json:"profile_image_url"`
	SubscriptionType      string     `json:"subscription_type"`
	SubscriptionCheckedAt *time.Time `json:"subscription_checked_at"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func newXAccountResponse(account xintegration.Account) xAccountResponse {
	return xAccountResponse{
		ID:                    account.ID,
		XUserID:               account.XUserID,
		Username:              account.Username,
		DisplayName:           account.DisplayName,
		ProfileImageURL:       account.ProfileImageURL,
		SubscriptionType:      account.SubscriptionType,
		SubscriptionCheckedAt: account.SubscriptionCheckedAt,
		CreatedAt:             account.CreatedAt,
		UpdatedAt:             account.UpdatedAt,
	}
}

func (api *API) disconnectX(response http.ResponseWriter, request *http.Request) {
	accountID, err := uuid.Parse(chi.URLParam(request, "accountID"))
	if err != nil {
		writeError(response, http.StatusNotFound, "x_account_not_found", "The X account was not found.", nil)
		return
	}
	if err := api.x.Disconnect(request.Context(), requestUserID(request), accountID); err != nil {
		if errors.Is(err, xintegration.ErrAccountNotFound) {
			writeError(response, http.StatusNotFound, "x_account_not_found", "The X account was not found.", nil)
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) redirectXResult(response http.ResponseWriter, request *http.Request, accountID, errorCode string) {
	target, err := url.Parse(api.frontendXRedirectURL)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
		return
	}
	query := target.Query()
	if errorCode == "" {
		query.Set("x_connection", "success")
		query.Set("account_id", accountID)
	} else {
		query.Set("x_connection", "error")
		query.Set("code", errorCode)
	}
	target.RawQuery = query.Encode()
	http.Redirect(response, request, target.String(), http.StatusSeeOther)
}
