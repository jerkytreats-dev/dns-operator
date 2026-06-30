package tailnetdns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const (
	DefaultOAuthScope    = "dns"
	DefaultOAuthTokenURL = DefaultAPIBaseURL + "/api/v2/oauth/token"
)

type AuthConfig struct {
	APIToken string
	OAuth    *OAuthClientCredentials
}

type OAuthClientCredentials struct {
	ClientID     string
	ClientSecret string
	Scopes       []string
	TokenURL     string
}

type authenticatedHTTPClient struct {
	client          *http.Client
	sensitiveValues func() []string
}

func (a AuthConfig) String() string {
	switch {
	case strings.TrimSpace(a.APIToken) != "":
		return "tailnetdns.AuthConfig{mode:bearer}"
	case a.OAuth != nil:
		return "tailnetdns.AuthConfig{mode:oauth_client_credentials}"
	default:
		return "tailnetdns.AuthConfig{mode:empty}"
	}
}

func (a AuthConfig) GoString() string {
	return a.String()
}

func (a AuthConfig) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprint(state, a.String())
}

func (a AuthConfig) sensitiveValues() []string {
	values := make([]string, 0, 3)
	if token := strings.TrimSpace(a.APIToken); token != "" {
		values = append(values, token)
	}
	if a.OAuth != nil {
		if clientID := strings.TrimSpace(a.OAuth.ClientID); clientID != "" {
			values = append(values, clientID)
		}
		if secret := strings.TrimSpace(a.OAuth.ClientSecret); secret != "" {
			values = append(values, secret)
		}
	}
	return values
}

func (o OAuthClientCredentials) String() string {
	return "tailnetdns.OAuthClientCredentials{redacted}"
}

func (o OAuthClientCredentials) GoString() string {
	return o.String()
}

func (o OAuthClientCredentials) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprint(state, o.String())
}

func NewAuthenticatedHTTPClient(auth AuthConfig) (*http.Client, error) {
	return NewAuthenticatedHTTPClientWithContext(context.Background(), auth)
}

func NewAuthenticatedHTTPClientWithContext(ctx context.Context, auth AuthConfig) (*http.Client, error) {
	authenticated, err := newAuthenticatedHTTPClientWithContext(ctx, auth)
	if err != nil {
		return nil, err
	}
	return authenticated.client, nil
}

func newAuthenticatedHTTPClientWithContext(ctx context.Context, auth AuthConfig) (authenticatedHTTPClient, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	hasAPIToken := strings.TrimSpace(auth.APIToken) != ""
	hasOAuth := auth.OAuth != nil
	if hasAPIToken == hasOAuth {
		return authenticatedHTTPClient{}, fmt.Errorf("exactly one Tailscale auth mode must be configured")
	}

	if hasAPIToken {
		return authenticatedHTTPClient{
			client:          newBearerHTTPClient(auth.APIToken, nil),
			sensitiveValues: auth.sensitiveValues,
		}, nil
	}

	oauth := *auth.OAuth
	if strings.TrimSpace(oauth.ClientID) == "" {
		return authenticatedHTTPClient{}, fmt.Errorf("oauth client id is required")
	}
	if strings.TrimSpace(oauth.ClientSecret) == "" {
		return authenticatedHTTPClient{}, fmt.Errorf("oauth client secret is required")
	}
	if strings.TrimSpace(oauth.TokenURL) == "" {
		oauth.TokenURL = DefaultOAuthTokenURL
	}

	scopes, err := cleanOAuthScopes(oauth.Scopes)
	if err != nil {
		return authenticatedHTTPClient{}, err
	}

	baseClient := &http.Client{Timeout: defaultTimeout}
	oauthCtx := context.WithValue(ctx, oauth2.HTTPClient, baseClient)
	config := clientcredentials.Config{
		ClientID:     strings.TrimSpace(oauth.ClientID),
		ClientSecret: strings.TrimSpace(oauth.ClientSecret),
		TokenURL:     strings.TrimSpace(oauth.TokenURL),
		Scopes:       scopes,
	}
	tokenSource := &trackingOAuthTokenSource{
		source: config.TokenSource(oauthCtx),
	}
	client := oauth2.NewClient(oauthCtx, sanitizingOAuthTokenSource{source: tokenSource})
	client.Timeout = defaultTimeout
	return authenticatedHTTPClient{
		client: client,
		sensitiveValues: func() []string {
			values := auth.sensitiveValues()
			return append(values, tokenSource.sensitiveValues()...)
		},
	}, nil
}

func newBearerHTTPClient(token string, base *http.Client) *http.Client {
	if base == nil {
		base = &http.Client{Timeout: defaultTimeout}
	}
	client := *base
	client.Transport = bearerAuthTransport{
		token: strings.TrimSpace(token),
		base:  client.Transport,
	}
	return &client
}

func cleanOAuthScopes(scopes []string) ([]string, error) {
	cleaned := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if scope != DefaultOAuthScope {
			return nil, fmt.Errorf("unsupported tailscale oauth scope %q", scope)
		}
		cleaned = append(cleaned, scope)
	}
	if len(cleaned) == 0 {
		return []string{DefaultOAuthScope}, nil
	}
	return cleaned, nil
}

type bearerAuthTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerAuthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", "Bearer "+t.token)
	return base.RoundTrip(cloned)
}

type sanitizingOAuthTokenSource struct {
	source oauth2.TokenSource
}

func (s sanitizingOAuthTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.source.Token()
	if err != nil {
		return nil, sanitizeOAuthTokenError(err)
	}
	return token, nil
}

type trackingOAuthTokenSource struct {
	source oauth2.TokenSource
	mu     sync.Mutex
	values []string
}

func (s *trackingOAuthTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.source.Token()
	if err != nil {
		return nil, err
	}

	var values []string
	if token != nil {
		if accessToken := strings.TrimSpace(token.AccessToken); accessToken != "" {
			values = append(values, accessToken)
		}
		if refreshToken := strings.TrimSpace(token.RefreshToken); refreshToken != "" {
			values = append(values, refreshToken)
		}
	}

	s.mu.Lock()
	s.values = values
	s.mu.Unlock()
	return token, nil
}

func (s *trackingOAuthTokenSource) sensitiveValues() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.values...)
}

func sanitizeOAuthTokenError(err error) error {
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) {
		message := "oauth2 token request failed"
		if retrieveErr.Response != nil {
			message = fmt.Sprintf("%s: status %d", message, retrieveErr.Response.StatusCode)
		}
		if code := safeOAuthErrorCode(retrieveErr.ErrorCode); code != "" {
			message += ": " + code
		}
		return errors.New(message)
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New("oauth2 token request failed")
}

func safeOAuthErrorCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	for _, char := range code {
		if char == '_' || char == '-' || char == '.' {
			continue
		}
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			continue
		}
		return ""
	}
	return code
}
