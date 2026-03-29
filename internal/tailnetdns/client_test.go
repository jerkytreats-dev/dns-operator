package tailnetdns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientUsesBearerAuthorization(t *testing.T) {
	t.Parallel()

	const token = "tskey-api-example"
	const nameserver = "100.100.100.100"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("authorization header = %q, want %q", got, "Bearer "+token)
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
