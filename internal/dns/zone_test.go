package dns

import (
	"strings"
	"testing"

	dnsv1alpha1 "github.com/jerkytreats/dns-operator/api/dns/v1alpha1"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	"github.com/jerkytreats/dns-operator/internal/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRecordForPublishedService(t *testing.T) {
	t.Parallel()

	policy := validation.MustNewZonePolicy([]string{"internal.example.test", "test.jerkytreats.dev"}, "internal.example.test")
	service := publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "dns-operator-system"},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "api.portal.internal.example.test",
			PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
			Backend: &publishv1alpha1.PublishBackend{
				Address: "192.0.2.10",
				Port:    8080,
			},
			TLS: &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
		},
	}

	record, err := RecordForPublishedService(policy, service)
	if err != nil {
		t.Fatalf("unexpected projection error: %v", err)
	}

	if record.Type != dnsv1alpha1.DNSRecordTypeA {
		t.Fatalf("expected A record, got %s", record.Type)
	}

	if record.Hostname != service.Spec.Hostname {
		t.Fatalf("expected hostname %s, got %s", service.Spec.Hostname, record.Hostname)
	}
}

func TestRenderZoneDeterministicAndNested(t *testing.T) {
	t.Parallel()

	policy := validation.MustNewZonePolicy([]string{"internal.example.test"}, "internal.example.test")
	zone, err := RenderZone(policy, []AuthoritativeRecord{
		{
			Hostname: "api.portal.internal.example.test",
			Type:     dnsv1alpha1.DNSRecordTypeA,
			TTL:      300,
			Values:   []string{"192.0.2.10"},
		},
		{
			Hostname: "app.internal.example.test",
			Type:     dnsv1alpha1.DNSRecordTypeCNAME,
			TTL:      300,
			Values:   []string{"backend.internal.example.test"},
		},
	})
	if err != nil {
		t.Fatalf("render zone: %v", err)
	}

	if !strings.Contains(zone.Content, "api.portal 300 IN A 192.0.2.10") {
		t.Fatalf("expected nested record in zone content:\n%s", zone.Content)
	}

	if !strings.Contains(zone.Content, "app 300 IN CNAME backend.internal.example.test.") {
		t.Fatalf("expected CNAME record in zone content:\n%s", zone.Content)
	}

	if zone.ConfigMapName != ZoneConfigMapName("internal.example.test") {
		t.Fatalf("expected configmap name %s, got %s", ZoneConfigMapName("internal.example.test"), zone.ConfigMapName)
	}

	if zone.Hash == "" {
		t.Fatal("expected non-empty zone hash")
	}
}

func TestRecordForPublishedServiceRejectsNonAuthoritativeZone(t *testing.T) {
	t.Parallel()

	policy := validation.MustNewZonePolicy([]string{"internal.example.test", "test.jerkytreats.dev"}, "internal.example.test")
	service := publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "dns-operator-system"},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "smoke.test.jerkytreats.dev",
			PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
			Backend: &publishv1alpha1.PublishBackend{
				Address: "192.0.2.10",
				Port:    8080,
			},
			TLS: &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
		},
	}

	if _, err := RecordForPublishedService(policy, service); err == nil {
		t.Fatal("expected non-authoritative hostname to be rejected")
	}
}
