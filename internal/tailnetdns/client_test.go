package tailnetdns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const testOAuthTokenPath = "/api/v2/oauth/token"

func TestHTTPClientUsesBearerAuthorization(t *testing.T) {
	t.Parallel()

	const token = "tskey-api-example\n"
	const nameserver = "100.100.100.100"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tskey-api-example" {
			t.Fatalf("authorization header = %q, want %q", got, "Bearer tskey-api-example")
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("accept header = %q, want application/json", got)
		}

		_, _ = w.Write([]byte(`{"internal.example.test":["` + nameserver + `"]}`))
	}))
	defer server.Close()

	client := NewHTTPClient("tail1cfaab.ts.net", token)
	client.baseURL = server.URL

	result, err := client.GetSplitDNS(context.Background())
	if err != nil {
		t.Fatalf("GetSplitDNS returned error: %v", err)
	}

	got := result["internal.example.test"]
	if len(got) != 1 || got[0] != nameserver {
		t.Fatalf("GetSplitDNS returned %v", result)
	}
}

func TestHTTPClientUsesOAuthClientCredentials(t *testing.T) {
	t.Parallel()

	const (
		clientID     = "oauth-client-id"
		clientSecret = "oauth-client-secret"
		accessToken  = "oauth-access-token"
		nameserver   = "100.100.100.100"
		tailnet      = "tail1cfaab.ts.net"
	)
	var tokenRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testOAuthTokenPath:
			tokenRequests.Add(1)
			if r.Method != http.MethodPost {
				t.Fatalf("token request method = %s, want POST", r.Method)
			}
			gotID, gotSecret, ok := r.BasicAuth()
			if !ok {
				t.Fatal("expected client credentials in basic auth")
			}
			if gotID != clientID {
				t.Fatalf("oauth client id = %q, want %q", gotID, clientID)
			}
			if gotSecret != clientSecret {
				t.Fatalf("oauth client secret = %q, want %q", gotSecret, clientSecret)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if got := r.PostForm.Get("grant_type"); got != "client_credentials" {
				t.Fatalf("grant_type = %q, want client_credentials", got)
			}
			if got := r.PostForm.Get("scope"); got != DefaultOAuthScope {
				t.Fatalf("scope = %q, want %q", got, DefaultOAuthScope)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"` + accessToken + `","token_type":"Bearer","expires_in":3600}`))
		case "/api/v2/tailnet/" + tailnet + "/dns/split-dns":
			if got := r.Header.Get("Authorization"); got != "Bearer "+accessToken {
				t.Fatalf("authorization header = %q, want oauth bearer token", got)
			}
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Fatalf("accept header = %q, want application/json", got)
			}
			_, _ = w.Write([]byte(`{"internal.example.test":["` + nameserver + `"]}`))
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClientWithAuthContext(context.Background(), tailnet, AuthConfig{
		OAuth: &OAuthClientCredentials{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     server.URL + testOAuthTokenPath,
		},
	})
	if err != nil {
		t.Fatalf("NewHTTPClientWithAuthContext returned error: %v", err)
	}
	client.baseURL = server.URL

	result, err := client.GetSplitDNS(context.Background())
	if err != nil {
		t.Fatalf("GetSplitDNS returned error: %v", err)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
	got := result["internal.example.test"]
	if len(got) != 1 || got[0] != nameserver {
		t.Fatalf("GetSplitDNS returned %v", result)
	}
}

func TestHTTPClientOAuthErrorsAreSanitized(t *testing.T) {
	t.Parallel()

	const (
		clientID     = "oauth-client-id"
		clientSecret = "oauth-client-secret"
		accessToken  = "oauth-access-token"
		tailnet      = "tail1cfaab.ts.net"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != testOAuthTokenPath {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"` + clientSecret + ` ` + accessToken + `"}`))
	}))
	defer server.Close()

	client, err := NewHTTPClientWithAuthContext(context.Background(), tailnet, AuthConfig{
		OAuth: &OAuthClientCredentials{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     server.URL + testOAuthTokenPath,
		},
	})
	if err != nil {
		t.Fatalf("NewHTTPClientWithAuthContext returned error: %v", err)
	}
	client.baseURL = server.URL

	_, err = client.GetSplitDNS(context.Background())
	if err == nil {
		t.Fatal("expected oauth failure")
	}

	message := err.Error()
	if !strings.Contains(message, "oauth2 token request failed: status 400: invalid_client") {
		t.Fatalf("unexpected sanitized error: %v", err)
	}
	for _, secret := range []string{clientSecret, accessToken, "error_description"} {
		if strings.Contains(message, secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}

func TestHTTPClientRejectsUnsupportedOAuthScope(t *testing.T) {
	t.Parallel()

	_, err := NewHTTPClientWithAuthContext(context.Background(), "tail1cfaab.ts.net", AuthConfig{
		OAuth: &OAuthClientCredentials{
			ClientID:     "oauth-client-id",
			ClientSecret: "oauth-client-secret",
			Scopes:       []string{"devices"},
			TokenURL:     "https://example.invalid/token",
		},
	})
	if err == nil {
		t.Fatal("expected unsupported scope error")
	}
	if !strings.Contains(err.Error(), `unsupported tailscale oauth scope "devices"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClientEscapesTailnetPathSegment(t *testing.T) {
	t.Parallel()

	const token = "tskey-api-example"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/api/v2/tailnet/tail%2Fwith%2Fslash/dns/split-dns" {
			t.Fatalf("escaped path = %q", got)
		}
		_, _ = w.Write([]byte(`{"internal.example.test":["100.100.100.100"]}`))
	}))
	defer server.Close()

	client := NewHTTPClient("tail/with/slash", token)
	client.baseURL = server.URL
	if _, err := client.GetSplitDNS(context.Background()); err != nil {
		t.Fatalf("GetSplitDNS returned error: %v", err)
	}
}

func TestAuthConfigFormattingRedactsSecrets(t *testing.T) {
	t.Parallel()

	const (
		apiToken     = "tskey-api-secret"
		clientID     = "oauth-client-id"
		clientSecret = "oauth-client-secret"
	)
	auth := AuthConfig{
		APIToken: apiToken,
		OAuth: &OAuthClientCredentials{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     "https://example.invalid/token",
		},
	}

	rendered := fmt.Sprintf("%v %+v %#v %v %+v %#v", auth, auth, auth, *auth.OAuth, *auth.OAuth, *auth.OAuth)
	for _, secret := range []string{apiToken, clientID, clientSecret} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("formatted auth config leaked %q: %s", secret, rendered)
		}
	}
}

func TestHTTPClientRedactsBearerTokenFromAPIError(t *testing.T) {
	t.Parallel()

	const token = "tskey-api-secret"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid token ` + token + `"}`))
	}))
	defer server.Close()

	client := NewHTTPClient("tail1cfaab.ts.net", token)
	client.baseURL = server.URL

	_, err := client.GetSplitDNS(context.Background())
	if err == nil {
		t.Fatal("expected API failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaked bearer token: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error did not include redaction marker: %v", err)
	}
}

func TestHTTPClientRedactsOAuthTokensFromAPIError(t *testing.T) {
	t.Parallel()

	const (
		clientID     = "oauth-client-id"
		clientSecret = "oauth-client-secret"
		accessToken  = "oauth-access-token"
		tailnet      = "tail1cfaab.ts.net"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testOAuthTokenPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"` + accessToken + `","token_type":"Bearer","expires_in":3600}`))
		case "/api/v2/tailnet/" + tailnet + "/dns/split-dns":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"denied ` + clientID + ` ` + clientSecret + ` ` + accessToken + `"}`))
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClientWithAuthContext(context.Background(), tailnet, AuthConfig{
		OAuth: &OAuthClientCredentials{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     server.URL + testOAuthTokenPath,
		},
	})
	if err != nil {
		t.Fatalf("NewHTTPClientWithAuthContext returned error: %v", err)
	}
	client.baseURL = server.URL

	_, err = client.GetSplitDNS(context.Background())
	if err == nil {
		t.Fatal("expected API failure")
	}
	for _, secret := range []string{clientID, clientSecret, accessToken} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error did not include redaction marker: %v", err)
	}
}
