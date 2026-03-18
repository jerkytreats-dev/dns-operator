package publish

import (
	"fmt"
	"strings"
	"testing"

	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	"github.com/jerkytreats/dns-operator/api/common"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestBuildRuntimeRendersStableCaddyfile(t *testing.T) {
	t.Parallel()

	services := []publishv1alpha1.PublishedService{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "media",
				Namespace: "dns-operator-system",
				Labels: map[string]string{
					"publish.jerkytreats.dev/certificate-bundle": "internal-shared",
				},
			},
			Spec: publishv1alpha1.PublishedServiceSpec{
				Hostname:    "media.internal.example.test",
				PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
				Backend: &publishv1alpha1.PublishBackend{
					Address:  "192.0.2.20",
					Port:     8443,
					Protocol: "https",
					Transport: &publishv1alpha1.PublishBackendTransport{
						InsecureSkipVerify: true,
					},
				},
				TLS: &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "app",
				Namespace: "dns-operator-system",
				Labels: map[string]string{
					"publish.jerkytreats.dev/certificate-bundle": "internal-shared",
				},
			},
			Spec: publishv1alpha1.PublishedServiceSpec{
				Hostname:    "app.internal.example.test",
				PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
				Backend: &publishv1alpha1.PublishBackend{
					Address:  "192.0.2.10",
					Port:     8080,
					Protocol: "http",
				},
				TLS: &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
			},
		},
	}
	bundle := certificatev1alpha1.CertificateBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-shared", Namespace: "dns-operator-system"},
		Spec: certificatev1alpha1.CertificateBundleSpec{
			PublishedServiceSelector: &common.ServiceSelector{
				MatchLabels: map[string]string{"publish.jerkytreats.dev/certificate-bundle": "internal-shared"},
			},
		},
		Status: certificatev1alpha1.CertificateBundleStatus{
			State: "Ready",
			EffectiveDomains: []string{
				"app.internal.example.test",
				"media.internal.example.test",
			},
			CertificateSecretRef: &common.ObjectReference{Name: "internal-example-test-shared-tls", Namespace: "dns-operator-system"},
		},
	}
	secrets := map[types.NamespacedName]*corev1.Secret{
		{Name: "internal-example-test-shared-tls", Namespace: "dns-operator-system"}: {
			ObjectMeta: metav1.ObjectMeta{Name: "internal-example-test-shared-tls", Namespace: "dns-operator-system"},
			Data: map[string][]byte{
				corev1.TLSCertKey:       []byte("cert"),
				corev1.TLSPrivateKeyKey: []byte("key"),
			},
		},
	}

	rendered, statuses, err := BuildRuntime(services, []certificatev1alpha1.CertificateBundle{bundle}, secrets, nil)
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}

	if rendered.ConfigMapName != RuntimeConfigMapName {
		t.Fatalf("unexpected configmap name: %s", rendered.ConfigMapName)
	}
	if len(rendered.CertificateSecretData) != 2 {
		t.Fatalf("expected aggregated certificate files, got %#v", rendered.CertificateSecretData)
	}

	if first := strings.Index(rendered.Content, "app.internal.example.test"); first == -1 {
		t.Fatalf("expected app host block in rendered config:\n%s", rendered.Content)
	} else if second := strings.Index(rendered.Content, "media.internal.example.test"); second == -1 || second < first {
		t.Fatalf("expected host blocks to be sorted by hostname:\n%s", rendered.Content)
	}
	if !strings.Contains(rendered.Content, "redir https://{host}{uri} permanent") {
		t.Fatalf("expected http redirect block in rendered config:\n%s", rendered.Content)
	}
	if !strings.Contains(rendered.Content, "transport http {\n\t\t\t\ttls_insecure_skip_verify") {
		t.Fatalf("expected insecure upstream transport block:\n%s", rendered.Content)
	}
	if strings.Count(rendered.Content, "tls /etc/dns-operator/certs/internal-example-test-shared-tls.crt /etc/dns-operator/certs/internal-example-test-shared-tls.key") != 2 {
		t.Fatalf("expected both host blocks to reference the shared cert files:\n%s", rendered.Content)
	}

	appStatus := statuses[types.NamespacedName{Name: "app", Namespace: "dns-operator-system"}]
	if appStatus.Err != nil {
		t.Fatalf("unexpected app status error: %v", appStatus.Err)
	}
	if appStatus.CertificateBundleRef == nil || appStatus.CertificateBundleRef.Name != "internal-shared" {
		t.Fatalf("expected bundle ref on app status, got %#v", appStatus.CertificateBundleRef)
	}
}

func TestBuildRuntimeReportsMissingCertificateCoverage(t *testing.T) {
	t.Parallel()

	service := publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: "dns-operator-system",
			Labels: map[string]string{
				"publish.jerkytreats.dev/certificate-bundle": "internal-shared",
			},
		},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "app.internal.example.test",
			PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
			Backend: &publishv1alpha1.PublishBackend{
				Address:  "192.0.2.10",
				Port:     8080,
				Protocol: "http",
			},
			TLS: &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
		},
	}
	bundle := certificatev1alpha1.CertificateBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-shared", Namespace: "dns-operator-system"},
		Spec: certificatev1alpha1.CertificateBundleSpec{
			PublishedServiceSelector: &common.ServiceSelector{
				MatchLabels: map[string]string{"publish.jerkytreats.dev/certificate-bundle": "internal-shared"},
			},
		},
		Status: certificatev1alpha1.CertificateBundleStatus{
			State:                "Ready",
			EffectiveDomains:     []string{"other.internal.example.test"},
			CertificateSecretRef: &common.ObjectReference{Name: "internal-example-test-shared-tls", Namespace: "dns-operator-system"},
		},
	}

	_, statuses, err := BuildRuntime(
		[]publishv1alpha1.PublishedService{service},
		[]certificatev1alpha1.CertificateBundle{bundle},
		map[types.NamespacedName]*corev1.Secret{},
		nil,
	)
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}

	status := statuses[types.NamespacedName{Name: "app", Namespace: "dns-operator-system"}]
	if status.Err == nil || !strings.Contains(status.Err.Error(), "does not yet cover hostname") {
		t.Fatalf("expected missing coverage error, got %v", status.Err)
	}
}

func TestBuildRuntimeRejectsDisallowedPublishZone(t *testing.T) {
	t.Parallel()

	service := publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "dns-operator-system"},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "app.example.com",
			PublishMode: publishv1alpha1.PublishModeDNSOnly,
		},
	}

	_, statuses, err := BuildRuntime(
		[]publishv1alpha1.PublishedService{service},
		nil,
		map[types.NamespacedName]*corev1.Secret{},
		func(hostname string) error {
			if hostname == "app.example.com" {
				return fmt.Errorf("hostname must be within one of internal.example.test, test.jerkytreats.dev")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}

	status := statuses[types.NamespacedName{Name: "app", Namespace: "dns-operator-system"}]
	if status.Err == nil {
		t.Fatal("expected zone validation error")
	}
}
