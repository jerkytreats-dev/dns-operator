package certificate

import (
	"context"
	"fmt"
	"testing"
	"time"

	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	"github.com/jerkytreats/dns-operator/api/common"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	certdomain "github.com/jerkytreats/dns-operator/internal/certificate"
	"github.com/jerkytreats/dns-operator/internal/validation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCertificateBundleReconcilePublishesSecret(t *testing.T) {
	t.Parallel()

	scheme := newCertificateScheme(t)
	bundle := &certificatev1alpha1.CertificateBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-shared", Namespace: "dns-operator-system", Generation: 1},
		Spec: certificatev1alpha1.CertificateBundleSpec{
			Mode: certificatev1alpha1.CertificateBundleModeSharedSAN,
			PublishedServiceSelector: &common.ServiceSelector{
				MatchLabels: map[string]string{"publish.jerkytreats.dev/certificate-bundle": "internal-shared"},
			},
			Issuer: certificatev1alpha1.BundleIssuer{
				Provider: certificatev1alpha1.CertificateIssuerLetsEncryptStaged,
				Email:    "admin@example.com",
			},
			Challenge: certificatev1alpha1.BundleChallenge{
				Type: certificatev1alpha1.CertificateChallengeDNS01,
				Cloudflare: certificatev1alpha1.BundleCloudflare{
					APITokenSecretRef: common.SecretKeyReference{Name: "cloudflare-credentials", Key: "api-token"},
				},
			},
			SecretTemplate: certificatev1alpha1.BundleSecretTemplate{Name: "internal-example-test-shared-tls"},
		},
	}
	service := &publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "app",
			Namespace:  "dns-operator-system",
			Generation: 1,
			Labels: map[string]string{
				"publish.jerkytreats.dev/certificate-bundle": "internal-shared",
			},
		},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "app.internal.example.test",
			PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
			TLS:         &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cloudflare-credentials", Namespace: "dns-operator-system"},
		Data: map[string][]byte{
			"api-token": []byte("token"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&certificatev1alpha1.CertificateBundle{}).
		WithObjects(bundle, service, secret).
		Build()

	reconciler := &CertificateBundleReconciler{Client: client, Scheme: scheme}
	reconciler.Issuer = fakeBundleIssuer{}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var publishedSecret corev1.Secret
	if err := client.Get(context.Background(), types.NamespacedName{Name: "internal-example-test-shared-tls", Namespace: "dns-operator-system"}, &publishedSecret); err != nil {
		t.Fatalf("get published secret: %v", err)
	}
	if len(publishedSecret.Data[corev1.TLSCertKey]) == 0 {
		t.Fatal("expected tls cert data")
	}

	var updated certificatev1alpha1.CertificateBundle
	if err := client.Get(context.Background(), types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace}, &updated); err != nil {
		t.Fatalf("get updated bundle: %v", err)
	}
	if updated.Status.State != "Ready" {
		t.Fatalf("expected Ready state, got %s", updated.Status.State)
	}
	if len(updated.Status.EffectiveDomains) != 1 || updated.Status.EffectiveDomains[0] != "app.internal.example.test" {
		t.Fatalf("unexpected effective domains: %#v", updated.Status.EffectiveDomains)
	}
}

func TestCertificateBundleReconcileMissingCredentials(t *testing.T) {
	t.Parallel()

	scheme := newCertificateScheme(t)
	bundle := &certificatev1alpha1.CertificateBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-shared", Namespace: "dns-operator-system", Generation: 1},
		Spec: certificatev1alpha1.CertificateBundleSpec{
			Mode: certificatev1alpha1.CertificateBundleModeSharedSAN,
			Issuer: certificatev1alpha1.BundleIssuer{
				Provider: certificatev1alpha1.CertificateIssuerLetsEncryptStaged,
				Email:    "admin@example.com",
			},
			Challenge: certificatev1alpha1.BundleChallenge{
				Type: certificatev1alpha1.CertificateChallengeDNS01,
				Cloudflare: certificatev1alpha1.BundleCloudflare{
					APITokenSecretRef: common.SecretKeyReference{Name: "missing", Key: "api-token"},
				},
			},
			SecretTemplate: certificatev1alpha1.BundleSecretTemplate{Name: "internal-example-test-shared-tls"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&certificatev1alpha1.CertificateBundle{}).
		WithObjects(bundle).
		Build()

	reconciler := &CertificateBundleReconciler{Client: client, Scheme: scheme}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
}

func TestCertificateBundleReconcileRejectsCrossNamespaceSecretRef(t *testing.T) {
	t.Parallel()

	scheme := newCertificateScheme(t)
	bundle := &certificatev1alpha1.CertificateBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-shared", Namespace: "dns-operator-system", Generation: 1},
		Spec: certificatev1alpha1.CertificateBundleSpec{
			Mode: certificatev1alpha1.CertificateBundleModeSharedSAN,
			Issuer: certificatev1alpha1.BundleIssuer{
				Provider: certificatev1alpha1.CertificateIssuerLetsEncryptStaged,
				Email:    "admin@example.com",
			},
			Challenge: certificatev1alpha1.BundleChallenge{
				Type: certificatev1alpha1.CertificateChallengeDNS01,
				Cloudflare: certificatev1alpha1.BundleCloudflare{
					APITokenSecretRef: common.SecretKeyReference{Name: "cloudflare-credentials", Namespace: "shared-secrets", Key: "api-token"},
				},
			},
			SecretTemplate: certificatev1alpha1.BundleSecretTemplate{Name: "internal-example-test-shared-tls"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&certificatev1alpha1.CertificateBundle{}).
		WithObjects(bundle).
		Build()

	reconciler := &CertificateBundleReconciler{Client: client, Scheme: scheme}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var updated certificatev1alpha1.CertificateBundle
	if err := client.Get(context.Background(), types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace}, &updated); err != nil {
		t.Fatalf("get updated bundle: %v", err)
	}
	if updated.Status.State != "Pending" {
		t.Fatalf("expected bundle to remain pending, got state %q", updated.Status.State)
	}
}

func TestCertificateBundleReconcilePersistsSecretPublishFailure(t *testing.T) {
	t.Parallel()

	scheme := newCertificateScheme(t)
	bundle := &certificatev1alpha1.CertificateBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-shared", Namespace: "dns-operator-system", Generation: 1},
		Spec: certificatev1alpha1.CertificateBundleSpec{
			Mode: certificatev1alpha1.CertificateBundleModeSharedSAN,
			PublishedServiceSelector: &common.ServiceSelector{
				MatchLabels: map[string]string{"publish.jerkytreats.dev/certificate-bundle": "internal-shared"},
			},
			Issuer: certificatev1alpha1.BundleIssuer{
				Provider: certificatev1alpha1.CertificateIssuerLetsEncryptStaged,
				Email:    "admin@example.com",
			},
			Challenge: certificatev1alpha1.BundleChallenge{
				Type: certificatev1alpha1.CertificateChallengeDNS01,
				Cloudflare: certificatev1alpha1.BundleCloudflare{
					APITokenSecretRef: common.SecretKeyReference{Name: "cloudflare-credentials", Key: "api-token"},
				},
			},
			SecretTemplate: certificatev1alpha1.BundleSecretTemplate{Name: ""},
		},
	}
	service := &publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "app",
			Namespace:  "dns-operator-system",
			Generation: 1,
			Labels: map[string]string{
				"publish.jerkytreats.dev/certificate-bundle": "internal-shared",
			},
		},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "app.internal.example.test",
			PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
			TLS:         &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cloudflare-credentials", Namespace: "dns-operator-system"},
		Data: map[string][]byte{
			"api-token": []byte("token"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&certificatev1alpha1.CertificateBundle{}).
		WithObjects(bundle, service, secret).
		Build()

	reconciler := &CertificateBundleReconciler{Client: client, Scheme: scheme, Issuer: fakeBundleIssuer{}}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var updated certificatev1alpha1.CertificateBundle
	if err := client.Get(context.Background(), types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace}, &updated); err != nil {
		t.Fatalf("get updated bundle: %v", err)
	}
	if updated.Status.State != "Pending" {
		t.Fatalf("expected bundle to remain pending, got state %q", updated.Status.State)
	}
	if len(updated.Status.Conditions) == 0 {
		t.Fatal("expected failure conditions to be persisted")
	}
}

func TestCertificateBundleReconcilePersistsCooldownAfterPreflightFailure(t *testing.T) {
	t.Parallel()

	scheme := newCertificateScheme(t)
	bundle := &certificatev1alpha1.CertificateBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "external-shared", Namespace: "dns-operator-system", Generation: 1},
		Spec: certificatev1alpha1.CertificateBundleSpec{
			Mode: certificatev1alpha1.CertificateBundleModeSharedSAN,
			PublishedServiceSelector: &common.ServiceSelector{
				MatchLabels: map[string]string{"publish.jerkytreats.dev/certificate-bundle": "external-shared"},
			},
			Issuer: certificatev1alpha1.BundleIssuer{
				Provider: certificatev1alpha1.CertificateIssuerLetsEncryptStaged,
				Email:    "admin@example.com",
			},
			Challenge: certificatev1alpha1.BundleChallenge{
				Type: certificatev1alpha1.CertificateChallengeDNS01,
				Cloudflare: certificatev1alpha1.BundleCloudflare{
					APITokenSecretRef: common.SecretKeyReference{Name: "cloudflare-credentials", Key: "api-token"},
				},
			},
			SecretTemplate: certificatev1alpha1.BundleSecretTemplate{Name: "external-shared-tls"},
		},
	}
	service := &publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "smoke",
			Namespace:  "dns-operator-system",
			Generation: 1,
			Labels: map[string]string{
				"publish.jerkytreats.dev/certificate-bundle": "external-shared",
			},
		},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "smoke.test.jerkytreats.dev",
			PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
			TLS:         &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cloudflare-credentials", Namespace: "dns-operator-system"},
		Data: map[string][]byte{
			"api-token": []byte("token"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&certificatev1alpha1.CertificateBundle{}).
		WithObjects(bundle, service, secret).
		Build()

	reconciler := &CertificateBundleReconciler{
		Client:     client,
		Scheme:     scheme,
		Issuer:     fakeErrorBundleIssuer{err: certdomain.WrapIssueError(certdomain.FailureClassDNSPreflight, fmt.Errorf("dns preflight failed"))},
		ZonePolicy: validation.MustNewZonePolicy([]string{"internal.example.test", "test.jerkytreats.dev"}, "internal.example.test"),
	}
	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace},
	})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 15*time.Minute {
		t.Fatalf("expected 15m requeue after preflight failure, got %v", result.RequeueAfter)
	}

	var updated certificatev1alpha1.CertificateBundle
	if err := client.Get(context.Background(), types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace}, &updated); err != nil {
		t.Fatalf("get updated bundle: %v", err)
	}
	if updated.Status.LastFailureClass != string(certdomain.FailureClassDNSPreflight) {
		t.Fatalf("unexpected failure class: %q", updated.Status.LastFailureClass)
	}
	if updated.Status.NextAttemptAt == nil {
		t.Fatal("expected next attempt at to be persisted")
	}
}

func newCertificateScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	if err := certificatev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add certificate scheme: %v", err)
	}
	if err := publishv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add publish scheme: %v", err)
	}
	return scheme
}

type fakeBundleIssuer struct {
	reuse bool
}

type fakeErrorBundleIssuer struct {
	err error
}

func (f fakeBundleIssuer) EnsureCertificate(_ context.Context, request certdomain.EnsureRequest) (certdomain.EnsureResult, error) {
	if request.Bundle.Spec.SecretTemplate.Name == "" {
		return certdomain.EnsureResult{}, fmt.Errorf("certificate secret template name is required")
	}
	if f.reuse && request.ExistingTLSSecret != nil {
		return certdomain.EnsureResult{
			TLSSecret: request.ExistingTLSSecret.DeepCopy(),
			Issued:    false,
		}, nil
	}
	secret, expiresAt, err := certdomain.BuildTLSSecret(request.Bundle.Spec.SecretTemplate.Name, request.Bundle.Namespace, request.Domains, 0)
	if err != nil {
		return certdomain.EnsureResult{}, err
	}
	accountSecret, err := certdomainTestAccountSecret(request.Bundle.Namespace, certdomain.AccountSecretName(request.Bundle.Name))
	if err != nil {
		return certdomain.EnsureResult{}, err
	}
	return certdomain.EnsureResult{
		TLSSecret:     secret,
		AccountSecret: accountSecret,
		ExpiresAt:     expiresAt,
		Issued:        true,
	}, nil
}

func (f fakeErrorBundleIssuer) EnsureCertificate(_ context.Context, _ certdomain.EnsureRequest) (certdomain.EnsureResult, error) {
	return certdomain.EnsureResult{}, f.err
}

func certdomainTestAccountSecret(namespace, name string) (*corev1.Secret, error) {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"registration.json": []byte("{}"),
			"private.key":       []byte("key"),
		},
	}, nil
}
