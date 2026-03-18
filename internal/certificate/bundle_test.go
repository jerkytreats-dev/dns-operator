package certificate

import (
	"testing"

	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	"github.com/jerkytreats/dns-operator/api/common"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEffectiveDomains(t *testing.T) {
	t.Parallel()

	bundle := certificatev1alpha1.CertificateBundle{
		Spec: certificatev1alpha1.CertificateBundleSpec{
			Mode: certificatev1alpha1.CertificateBundleModeSharedSAN,
			PublishedServiceSelector: &common.ServiceSelector{
				MatchLabels: map[string]string{
					"publish.jerkytreats.dev/certificate-bundle": "internal-shared",
				},
			},
			AdditionalDomains: []string{"internal.example.test"},
		},
	}

	services := []publishv1alpha1.PublishedService{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "app",
				Labels: map[string]string{
					"publish.jerkytreats.dev/certificate-bundle": "internal-shared",
				},
			},
			Spec: publishv1alpha1.PublishedServiceSpec{
				Hostname:    "app.internal.example.test",
				PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
				TLS:         &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ignored",
			},
			Spec: publishv1alpha1.PublishedServiceSpec{
				Hostname:    "ignored.internal.example.test",
				PublishMode: publishv1alpha1.PublishModeDNSOnly,
			},
		},
	}

	domains, err := EffectiveDomains(bundle, services, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(domains))
	}
	if domains[0] != "app.internal.example.test" || domains[1] != "internal.example.test" {
		t.Fatalf("unexpected domains: %#v", domains)
	}
}

func TestBuildTLSSecret(t *testing.T) {
	t.Parallel()

	secret, expiresAt, err := BuildTLSSecret("bundle-tls", "dns-operator-system", []string{"app.internal.example.test"}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret.Type != "kubernetes.io/tls" {
		t.Fatalf("unexpected secret type: %s", secret.Type)
	}
	if len(secret.Data["tls.crt"]) == 0 || len(secret.Data["tls.key"]) == 0 {
		t.Fatal("expected tls data in secret")
	}
	if expiresAt == nil {
		t.Fatal("expected expiry time")
	}
}
