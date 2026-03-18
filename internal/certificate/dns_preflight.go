package certificate

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	defaultPreflightTimeout  = 2 * time.Minute
	defaultPreflightInterval = 5 * time.Second
)

type cloudflareAPI struct {
	httpClient *http.Client
	token      string
}

type cloudflareZonesResponse struct {
	Success bool                 `json:"success"`
	Errors  []cloudflareAPIError `json:"errors"`
	Result  []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"result"`
}

type cloudflareCreateRecordRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

type cloudflareRecordResponse struct {
	Success bool                 `json:"success"`
	Errors  []cloudflareAPIError `json:"errors"`
	Result  struct {
		ID string `json:"id"`
	} `json:"result"`
}

type cloudflareAPIError struct {
	Message string `json:"message"`
}

type preflightRecord struct {
	zoneID   string
	recordID string
	fqdn     string
	value    string
}

func preflightDNSChallenges(ctx context.Context, token string, domains []string, resolvers []string) error {
	if token == "" {
		return fmt.Errorf("cloudflare api token is required")
	}
	if len(domains) == 0 {
		return fmt.Errorf("at least one domain is required")
	}

	client := &cloudflareAPI{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
	}
	records := make([]preflightRecord, 0, len(domains))
	defer func() {
		_ = cleanupPreflightRecords(ctx, client, records)
	}()

	for _, domain := range domains {
		zoneID, err := client.findZoneID(ctx, domain)
		if err != nil {
			return fmt.Errorf("resolve cloudflare zone for %s: %w", domain, err)
		}
		value, err := randomPreflightValue()
		if err != nil {
			return err
		}
		fqdn := "_acme-challenge." + domain
		recordID, err := client.createTXTRecord(ctx, zoneID, fqdn, value)
		if err != nil {
			return fmt.Errorf("create preflight txt record for %s: %w", domain, err)
		}
		records = append(records, preflightRecord{
			zoneID:   zoneID,
			recordID: recordID,
			fqdn:     fqdn,
			value:    value,
		})
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, defaultPreflightTimeout)
	defer cancel()
	for {
		if err := verifyPreflightRecords(deadlineCtx, records, resolvers); err == nil {
			if err := cleanupPreflightRecords(ctx, client, records); err != nil {
				return fmt.Errorf("cleanup preflight txt records: %w", err)
			}
			records = nil
			return nil
		} else if deadlineCtx.Err() != nil {
			return fmt.Errorf("dns preflight propagation did not converge: %w", err)
		}

		select {
		case <-time.After(defaultPreflightInterval):
		case <-deadlineCtx.Done():
			return fmt.Errorf("dns preflight propagation timed out: %w", deadlineCtx.Err())
		}
	}
}

func cleanupPreflightRecords(ctx context.Context, client *cloudflareAPI, records []preflightRecord) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var firstErr error
	for _, record := range records {
		if err := client.deleteTXTRecord(cleanupCtx, record.zoneID, record.recordID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func verifyPreflightRecords(ctx context.Context, records []preflightRecord, resolvers []string) error {
	for _, record := range records {
		for _, resolver := range resolvers {
			values, err := lookupTXT(ctx, resolver, record.fqdn)
			if err != nil {
				return fmt.Errorf("%s via %s: %w", record.fqdn, resolver, err)
			}
			if !containsString(values, record.value) {
				return fmt.Errorf("%s via %s missing propagated value", record.fqdn, resolver)
			}
		}
	}
	return nil
}

func lookupTXT(ctx context.Context, resolverAddress, fqdn string) ([]string, error) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, "udp", resolverAddress)
		},
	}
	values, err := resolver.LookupTXT(ctx, fqdn)
	if err != nil {
		return nil, err
	}
	return values, nil
}

func (c *cloudflareAPI) findZoneID(ctx context.Context, hostname string) (string, error) {
	labels := strings.Split(hostname, ".")
	for i := 0; i < len(labels)-1; i++ {
		zoneName := strings.Join(labels[i:], ".")
		response, err := c.get(ctx, "https://api.cloudflare.com/client/v4/zones?name="+zoneName)
		if err != nil {
			return "", err
		}
		if len(response.Result) > 0 {
			return response.Result[0].ID, nil
		}
	}
	return "", fmt.Errorf("no accessible cloudflare zone found")
}

func (c *cloudflareAPI) createTXTRecord(ctx context.Context, zoneID, fqdn, value string) (string, error) {
	body, err := c.post(ctx, "https://api.cloudflare.com/client/v4/zones/"+zoneID+"/dns_records", cloudflareCreateRecordRequest{
		Type:    "TXT",
		Name:    fqdn,
		Content: value,
		TTL:     60,
	})
	if err != nil {
		return "", err
	}
	if body.Result.ID == "" {
		return "", fmt.Errorf("cloudflare did not return a record id")
	}
	return body.Result.ID, nil
}

func (c *cloudflareAPI) deleteTXTRecord(ctx context.Context, zoneID, recordID string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, "https://api.cloudflare.com/client/v4/zones/"+zoneID+"/dns_records/"+recordID, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("cloudflare delete failed with status %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *cloudflareAPI) get(ctx context.Context, url string) (cloudflareZonesResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return cloudflareZonesResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return cloudflareZonesResponse{}, err
	}
	defer response.Body.Close()

	var payload cloudflareZonesResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return cloudflareZonesResponse{}, err
	}
	if !payload.Success {
		return cloudflareZonesResponse{}, errors.New(cloudflareErrors(payload.Errors))
	}
	return payload, nil
}

func (c *cloudflareAPI) post(ctx context.Context, url string, payload cloudflareCreateRecordRequest) (cloudflareRecordResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return cloudflareRecordResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return cloudflareRecordResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return cloudflareRecordResponse{}, err
	}
	defer response.Body.Close()

	var recordResponse cloudflareRecordResponse
	if err := json.NewDecoder(response.Body).Decode(&recordResponse); err != nil {
		return cloudflareRecordResponse{}, err
	}
	if !recordResponse.Success {
		return cloudflareRecordResponse{}, errors.New(cloudflareErrors(recordResponse.Errors))
	}
	return recordResponse, nil
}

func cloudflareErrors(errors []cloudflareAPIError) string {
	if len(errors) == 0 {
		return "cloudflare api request failed"
	}
	messages := make([]string, 0, len(errors))
	for _, err := range errors {
		if err.Message != "" {
			messages = append(messages, err.Message)
		}
	}
	if len(messages) == 0 {
		return "cloudflare api request failed"
	}
	return strings.Join(messages, "; ")
}

func randomPreflightValue() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate preflight token: %w", err)
	}
	return "dns-operator-preflight-" + hex.EncodeToString(buf), nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
