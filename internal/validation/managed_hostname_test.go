package validation

import "testing"

func TestValidateManagedHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		wantErr  bool
	}{
		{name: "valid nested hostname", hostname: "api.portal.internal.example.test"},
		{name: "valid simple hostname", hostname: "app.internal.example.test"},
		{name: "reject uppercase", hostname: "App.internal.example.test", wantErr: true},
		{name: "reject trailing dot", hostname: "app.internal.example.test.", wantErr: true},
		{name: "reject wrong zone", hostname: "app.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateManagedHostname(tt.hostname)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.hostname)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.hostname, err)
			}
		})
	}
}

func TestZonePolicyValidatePublishedHostname(t *testing.T) {
	t.Parallel()

	policy := MustNewZonePolicy([]string{"internal.example.test", "test.jerkytreats.dev"}, "internal.example.test")
	tests := []struct {
		name     string
		hostname string
		wantErr  bool
	}{
		{name: "internal hostname", hostname: "app.internal.example.test"},
		{name: "configured external zone hostname", hostname: "smoke.test.jerkytreats.dev"},
		{name: "reject wrong zone", hostname: "app.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := policy.ValidatePublishedHostname(tt.hostname)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.hostname)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.hostname, err)
			}
		})
	}
}

func TestZonePolicyValidateAuthoritativeHostname(t *testing.T) {
	t.Parallel()

	policy := MustNewZonePolicy([]string{"internal.example.test", "test.jerkytreats.dev"}, "internal.example.test")
	if err := policy.ValidateAuthoritativeHostname("app.internal.example.test"); err != nil {
		t.Fatalf("expected internal zone hostname to be authoritative: %v", err)
	}
	if err := policy.ValidateAuthoritativeHostname("smoke.test.jerkytreats.dev"); err == nil {
		t.Fatal("expected external publish zone hostname to be rejected for authoritative dns")
	}
}

func TestZonePolicyRelativeName(t *testing.T) {
	t.Parallel()

	policy := MustNewZonePolicy([]string{"internal.example.test"}, "internal.example.test")
	relativeName, err := policy.RelativeName("api.portal.internal.example.test")
	if err != nil {
		t.Fatalf("unexpected relative name error: %v", err)
	}
	if relativeName != "api.portal" {
		t.Fatalf("expected api.portal, got %q", relativeName)
	}
}

func TestInferRecordFromAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		address string
		want    string
		wantErr bool
	}{
		{name: "ipv4", address: "192.0.2.10", want: "A"},
		{name: "ipv6", address: "2001:db8::10", want: "AAAA"},
		{name: "fqdn", address: "backend.internal.example.test", want: "CNAME"},
		{name: "invalid", address: "bad host", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, _, err := InferRecordFromAddress(tt.address)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.address)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.address, err)
			}
			if got != tt.want {
				t.Fatalf("expected type %q, got %q", tt.want, got)
			}
		})
	}
}
