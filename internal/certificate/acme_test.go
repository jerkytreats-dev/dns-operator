package certificate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/lego"
	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAccountSecretRoundTrip(t *testing.T) {
	t.Parallel()

	user, accountSecret, err := loadOrCreateACMEUser("dns-operator-system", AccountSecretName("internal-shared"), "admin@example.com", nil)
	if err != nil {
		t.Fatalf("loadOrCreateACMEUser returned error: %v", err)
	}
	if accountSecret.Name != "internal-shared-acme-account" {
		t.Fatalf("unexpected account secret name: %s", accountSecret.Name)
	}
	if user.Registration != nil {
		t.Fatalf("expected registration to be nil before account registration, got %#v", user.Registration)
	}

	loadedUser, err := acmeUserFromSecret(accountSecret, "admin@example.com")
	if err != nil {
		t.Fatalf("acmeUserFromSecret returned error: %v", err)
	}
	if loadedUser.GetPrivateKey() == nil {
		t.Fatal("expected loaded private key")
	}
}

func TestReusableCertificate(t *testing.T) {
	t.Parallel()

	secret, expiresAt, err := BuildTLSSecret("bundle-tls", "dns-operator-system", []string{"app.internal.example.test"}, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("BuildTLSSecret returned error: %v", err)
	}

	reusable, notAfter, err := reusableCertificate(secret, []string{"app.internal.example.test"}, 30*24*time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("reusableCertificate returned error: %v", err)
	}
	if !reusable {
		t.Fatal("expected certificate to be reusable")
	}
	if expiresAt == nil || notAfter.Sub(*expiresAt) > time.Second || expiresAt.Sub(notAfter) > time.Second {
		t.Fatalf("expected matching expiry, got %v and %v", notAfter, expiresAt)
	}
}

func TestReusableCertificateRequiresCoverage(t *testing.T) {
	t.Parallel()

	secret, _, err := BuildTLSSecret("bundle-tls", "dns-operator-system", []string{"app.internal.example.test"}, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("BuildTLSSecret returned error: %v", err)
	}

	reusable, _, err := reusableCertificate(secret, []string{"other.internal.example.test"}, 30*24*time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("reusableCertificate returned error: %v", err)
	}
	if reusable {
		t.Fatal("expected certificate coverage mismatch to force reissuance")
	}
}

func TestReusableCertificateRejectsMissingData(t *testing.T) {
	t.Parallel()

	_, _, err := reusableCertificate(&corev1.Secret{}, []string{"app.internal.example.test"}, defaultRenewBefore, time.Now().UTC())
	if err == nil {
		t.Fatal("expected missing data error")
	}
}

func TestEnsureCertificateReturnsPreflightFailureWithoutIssuance(t *testing.T) {
	t.Parallel()

	issuer := NewACMEIssuer()
	issuer.preflight = func(_ context.Context, _ string, _ []string, _ []string) error {
		return fmt.Errorf("txt record never propagated")
	}
	issuer.newClient = func(_ *acmeUser, _, _ string) (*lego.Client, error) {
		t.Fatal("expected preflight failure to stop before ACME client creation")
		return nil, nil
	}

	_, err := issuer.EnsureCertificate(context.Background(), EnsureRequest{
		Bundle: certificatev1alpha1.CertificateBundle{
			ObjectMeta: metav1.ObjectMeta{Name: "smoke", Namespace: "dns-operator-system"},
			Spec: certificatev1alpha1.CertificateBundleSpec{
				Issuer:         certificatev1alpha1.BundleIssuer{Provider: certificatev1alpha1.CertificateIssuerLetsEncryptStaged, Email: "admin@example.com"},
				SecretTemplate: certificatev1alpha1.BundleSecretTemplate{Name: "smoke-tls"},
			},
		},
		Domains:            []string{"smoke.test.jerkytreats.dev"},
		CloudflareAPIToken: "token",
	})
	if FailureClassFromError(err) != FailureClassDNSPreflight {
		t.Fatalf("expected dns preflight failure, got %v", err)
	}
}

func TestCooldownForFailure(t *testing.T) {
	t.Parallel()

	if got := CooldownForFailure(FailureClassDNSPreflight, 1); got != 15*time.Minute {
		t.Fatalf("unexpected preflight cooldown: %v", got)
	}
	if got := CooldownForFailure(FailureClassRateLimited, 2); got != 12*time.Hour {
		t.Fatalf("unexpected rate limit cooldown: %v", got)
	}
}
