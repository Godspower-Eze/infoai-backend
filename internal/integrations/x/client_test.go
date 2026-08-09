package xintegration

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestXClientUsesPKCEAndRequiredScopes(t *testing.T) {
	client := NewXClient("client-id", "client-secret", "https://app.example/x/callback", http.DefaultClient, XEndpoints{
		AuthorizationURL: "https://x.example/authorize",
		TokenURL:         "https://x.example/token",
		APIBaseURL:       "https://api.x.example",
		RevokeURL:        "https://x.example/revoke",
	})

	rawURL := client.AuthorizationURL("state-value", "challenge-value")
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	query := parsed.Query()
	if query.Get("state") != "state-value" || query.Get("code_challenge") != "challenge-value" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization PKCE query = %v", query)
	}
	if got, want := strings.Fields(query.Get("scope")), RequiredScopes; !reflect.DeepEqual(got, want) {
		t.Fatalf("authorization scopes = %v, want %v", got, want)
	}
}

func TestXClientExchangesFetchesRefreshesAndRevokes(t *testing.T) {
	var exchangeVerifier, refreshToken, revokedToken string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/token":
			if username, password, ok := r.BasicAuth(); !ok || username != "client-id" || password != "client-secret" {
				t.Errorf("token BasicAuth = %q/%q/%v", username, password, ok)
			}
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm() error = %v", err)
			}
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				exchangeVerifier = r.Form.Get("code_verifier")
				return jsonResponse(http.StatusOK, `{"access_token":"access-one","refresh_token":"refresh-one","token_type":"bearer","expires_in":7200,"scope":"tweet.read users.read"}`), nil
			case "refresh_token":
				refreshToken = r.Form.Get("refresh_token")
				return jsonResponse(http.StatusOK, `{"access_token":"access-two","refresh_token":"refresh-two","token_type":"bearer","expires_in":7200}`), nil
			default:
				return jsonResponse(http.StatusBadRequest, `{"error":"unexpected_grant"}`), nil
			}
		case "/2/users/me":
			if r.Header.Get("Authorization") != "Bearer access-one" {
				t.Errorf("profile Authorization = %q", r.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{"data":{"id":"x-123","username":"person","name":"A Person","profile_image_url":"https://images.example/person.jpg"}}`), nil
		case "/revoke":
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm() error = %v", err)
			}
			revokedToken = r.Form.Get("token")
			return jsonResponse(http.StatusOK, `{}`), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	})}

	client := NewXClient("client-id", "client-secret", "https://app.example/x/callback", httpClient, XEndpoints{
		AuthorizationURL: "https://x.example/authorize",
		TokenURL:         "https://x.example/token",
		APIBaseURL:       "https://x.example",
		RevokeURL:        "https://x.example/revoke",
	})
	ctx := context.Background()
	exchanged, err := client.Exchange(ctx, "auth-code", "pkce-verifier")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if exchanged.AccessToken != "access-one" || exchanged.RefreshToken != "refresh-one" || exchangeVerifier != "pkce-verifier" {
		t.Fatalf("Exchange() = %+v, verifier = %q", exchanged, exchangeVerifier)
	}
	if got, want := exchanged.Scopes, []string{"tweet.read", "users.read"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Exchange() scopes = %v, want %v", got, want)
	}

	profile, err := client.CurrentUser(ctx, exchanged.AccessToken)
	if err != nil {
		t.Fatalf("CurrentUser() error = %v", err)
	}
	if profile.ID != "x-123" || profile.Username != "person" || profile.ProfileImageURL == nil {
		t.Fatalf("CurrentUser() = %+v", profile)
	}

	refreshed, err := client.Refresh(ctx, exchanged.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed.AccessToken != "access-two" || refreshed.RefreshToken != "refresh-two" || refreshToken != "refresh-one" {
		t.Fatalf("Refresh() = %+v, input = %q", refreshed, refreshToken)
	}
	if refreshed.Expiry.Before(time.Now().Add(time.Hour)) {
		t.Fatalf("Refresh() expiry = %v, want about two hours", refreshed.Expiry)
	}

	if err := client.Revoke(ctx, refreshed.RefreshToken); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if revokedToken != "refresh-two" {
		t.Fatalf("Revoke() token = %q, want refresh-two", revokedToken)
	}
}
