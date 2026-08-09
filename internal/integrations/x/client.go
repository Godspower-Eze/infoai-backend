package xintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

var RequiredScopes = []string{"tweet.read", "users.read", "tweet.write", "offline.access"}

type XEndpoints struct {
	AuthorizationURL string
	TokenURL         string
	APIBaseURL       string
	RevokeURL        string
}

func DefaultXEndpoints() XEndpoints {
	return XEndpoints{
		AuthorizationURL: "https://x.com/i/oauth2/authorize",
		TokenURL:         "https://api.x.com/2/oauth2/token",
		APIBaseURL:       "https://api.x.com",
		RevokeURL:        "https://api.x.com/2/oauth2/revoke",
	}
}

type XClient struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	config       *oauth2.Config
	endpoints    XEndpoints
}

func NewXClient(clientID, clientSecret, callbackURL string, httpClient *http.Client, endpoints XEndpoints) *XClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &XClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   httpClient,
		endpoints:    endpoints,
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  callbackURL,
			Scopes:       append([]string(nil), RequiredScopes...),
			Endpoint: oauth2.Endpoint{
				AuthURL:   endpoints.AuthorizationURL,
				TokenURL:  endpoints.TokenURL,
				AuthStyle: oauth2.AuthStyleInHeader,
			},
		},
	}
}

func (c *XClient) AuthorizationURL(state, challenge string) string {
	return c.config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

func (c *XClient) Exchange(ctx context.Context, code, verifier string) (OAuthToken, error) {
	token, err := c.config.Exchange(c.withHTTPClient(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return OAuthToken{}, err
	}
	return c.oauthToken(token), nil
}

func (c *XClient) CurrentUser(ctx context.Context, accessToken string) (Profile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.endpoints.APIBaseURL, "/")+"/2/users/me?user.fields=profile_image_url", nil)
	if err != nil {
		return Profile{}, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Profile{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Profile{}, fmt.Errorf("X profile request returned %s", response.Status)
	}
	var payload struct {
		Data struct {
			ID              string  `json:"id"`
			Username        string  `json:"username"`
			Name            string  `json:"name"`
			ProfileImageURL *string `json:"profile_image_url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return Profile{}, fmt.Errorf("decode X profile: %w", err)
	}
	if payload.Data.ID == "" || payload.Data.Username == "" {
		return Profile{}, fmt.Errorf("X profile response is missing identity fields")
	}
	return Profile{ID: payload.Data.ID, Username: payload.Data.Username, DisplayName: payload.Data.Name, ProfileImageURL: payload.Data.ProfileImageURL}, nil
}

func (c *XClient) Refresh(ctx context.Context, refreshToken string) (OAuthToken, error) {
	source := c.config.TokenSource(c.withHTTPClient(ctx), &oauth2.Token{RefreshToken: refreshToken, Expiry: time.Now().Add(-time.Hour)})
	token, err := source.Token()
	if err != nil {
		return OAuthToken{}, err
	}
	return c.oauthToken(token), nil
}

func (c *XClient) Revoke(ctx context.Context, token string) error {
	form := url.Values{"token": {token}, "client_id": {c.clientID}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoints.RevokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.clientSecret != "" {
		request.SetBasicAuth(c.clientID, c.clientSecret)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("X token revocation returned %s", response.Status)
	}
	return nil
}

func (c *XClient) withHTTPClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
}

func (c *XClient) oauthToken(token *oauth2.Token) OAuthToken {
	scopes := append([]string(nil), RequiredScopes...)
	if raw, ok := token.Extra("scope").(string); ok && raw != "" {
		scopes = strings.Fields(raw)
	}
	return OAuthToken{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, Expiry: token.Expiry, Scopes: scopes}
}

var _ Provider = (*XClient)(nil)
