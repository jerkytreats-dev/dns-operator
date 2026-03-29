package tailnetdns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	tailnet    string
	apiToken   string
	baseURL    string
	httpClient *http.Client
}

func NewHTTPClient(tailnet, apiToken string) *HTTPClient {
	return &HTTPClient{
		tailnet:  tailnet,
		apiToken: strings.TrimSpace(apiToken),
		baseURL:  DefaultAPIBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
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

	url := strings.TrimSuffix(c.baseURL, "/") + fmt.Sprintf(splitDNSEndpoint, c.tailnet)
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
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
		return nil, fmt.Errorf("tailscale API returned status %d", resp.StatusCode)
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
