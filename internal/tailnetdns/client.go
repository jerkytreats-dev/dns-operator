package tailnetdns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultAPIBaseURL = "https://api.tailscale.com"
	splitDNSEndpoint  = "/api/v2/tailnet/%s/dns/split-dns"
	defaultTimeout    = 30 * time.Second
)

type SplitDNSClient interface {
	GetSplitDNS(ctx context.Context) (map[string][]string, error)
	PatchSplitDNS(ctx context.Context, changes map[string]any) (map[string][]string, error)
}

type HTTPClient struct {
	tailnet         string
	baseURL         string
	httpClient      *http.Client
	sensitiveValues func() []string
}

func NewHTTPClient(tailnet, apiToken string) *HTTPClient {
	client := NewHTTPClientWithHTTPClient(tailnet, newBearerHTTPClient(apiToken, nil))
	client.sensitiveValues = AuthConfig{APIToken: apiToken}.sensitiveValues
	return client
}

func NewHTTPClientWithAuth(tailnet string, auth AuthConfig) (*HTTPClient, error) {
	return NewHTTPClientWithAuthContext(context.Background(), tailnet, auth)
}

func NewHTTPClientWithAuthContext(ctx context.Context, tailnet string, auth AuthConfig) (*HTTPClient, error) {
	authenticated, err := newAuthenticatedHTTPClientWithContext(ctx, auth)
	if err != nil {
		return nil, err
	}
	client := NewHTTPClientWithHTTPClient(tailnet, authenticated.client)
	client.sensitiveValues = authenticated.sensitiveValues
	return client, nil
}

func NewHTTPClientWithHTTPClient(tailnet string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HTTPClient{
		tailnet:    tailnet,
		baseURL:    DefaultAPIBaseURL,
		httpClient: httpClient,
	}
}

func (c *HTTPClient) GetSplitDNS(ctx context.Context) (map[string][]string, error) {
	req, err := c.newRequest(ctx, http.MethodGet, nil)
	if err != nil {
		return nil, err
	}

	return c.do(req)
}

func (c *HTTPClient) PatchSplitDNS(ctx context.Context, changes map[string]any) (map[string][]string, error) {
	body, err := json.Marshal(changes)
	if err != nil {
		return nil, fmt.Errorf("marshal split dns patch: %w", err)
	}

	req, err := c.newRequest(ctx, http.MethodPatch, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	return c.do(req)
}

func (c *HTTPClient) newRequest(ctx context.Context, method string, body *bytes.Reader) (*http.Request, error) {
	var reader *bytes.Reader
	if body != nil {
		reader = body
	} else {
		reader = bytes.NewReader(nil)
	}

	tailnet := strings.TrimSpace(c.tailnet)
	if tailnet == "" {
		return nil, fmt.Errorf("tailnet cannot be empty")
	}
	requestURL := strings.TrimSuffix(c.baseURL, "/") + fmt.Sprintf(splitDNSEndpoint, url.PathEscape(tailnet))
	req, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *HTTPClient) do(req *http.Request) (map[string][]string, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tailscale API returned status %d%s", resp.StatusCode, c.responseErrorSuffix(resp.Body))
	}

	var result map[string][]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result == nil {
		result = map[string][]string{}
	}
	return result, nil
}

func (c *HTTPClient) responseErrorSuffix(body io.Reader) string {
	if body == nil {
		return ""
	}

	raw, err := io.ReadAll(io.LimitReader(body, 512))
	if err != nil {
		return ""
	}
	message := strings.TrimSpace(string(raw))
	if message == "" {
		return ""
	}

	var parsed struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err == nil {
		switch {
		case strings.TrimSpace(parsed.Message) != "":
			message = strings.TrimSpace(parsed.Message)
		case strings.TrimSpace(parsed.Error) != "":
			message = strings.TrimSpace(parsed.Error)
		}
	}

	return ": " + sanitizeAPIMessage(message, c.currentSensitiveValues())
}

func sanitizeAPIMessage(message string, sensitiveValues []string) string {
	for _, value := range sensitiveValues {
		if value == "" {
			continue
		}
		message = strings.ReplaceAll(message, value, "[redacted]")
	}
	return strings.Map(func(value rune) rune {
		switch value {
		case '\n', '\r', '\t':
			return ' '
		}
		if value < 0x20 || value == 0x7f {
			return -1
		}
		return value
	}, message)
}

func (c *HTTPClient) currentSensitiveValues() []string {
	if c.sensitiveValues == nil {
		return nil
	}
	return c.sensitiveValues()
}
