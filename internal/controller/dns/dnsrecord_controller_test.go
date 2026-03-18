package dns

import (
	"context"
	"strings"
	"testing"

	"github.com/jerkytreats/dns-operator/api/common"
	dnsv1alpha1 "github.com/jerkytreats/dns-operator/api/dns/v1alpha1"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	dnsdomain "github.com/jerkytreats/dns-operator/internal/dns"
	"github.com/jerkytreats/dns-operator/internal/validation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileRendersZoneConfigMap(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	record := &dnsv1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "app-a",
			Namespace:  "dns-operator-system",
			Generation: 1,
		},
		Spec: dnsv1alpha1.DNSRecordSpec{
			Hostname: "app.internal.example.test",
			Type:     dnsv1alpha1.DNSRecordTypeA,
			TTL:      300,
			Values:   []string{"192.0.2.10"},
		},
	}
	service := &publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "api",
			Namespace:  "dns-operator-system",
			Generation: 1,
		},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "api.portal.internal.example.test",
			PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
			Backend: &publishv1alpha1.PublishBackend{
				Address: "192.0.2.11",
				Port:    8080,
			},
			TLS: &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
		},
	}
	runtimeService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dns-operator-caddy",
			Namespace: "dns-operator-system",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "192.0.2.200",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&dnsv1alpha1.DNSRecord{}, &publishv1alpha1.PublishedService{}).
		WithObjects(record, service, runtimeService).
		Build()

	reconciler := &DNSRecordReconciler{
		Client: client,
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: record.Name, Namespace: record.Namespace},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var configMap corev1.ConfigMap
	if err := client.Get(context.Background(), types.NamespacedName{
		Name:      dnsdomain.ZoneConfigMapName("internal.example.test"),
		Namespace: "dns-operator-system",
	}, &configMap); err != nil {
		t.Fatalf("get configmap: %v", err)
	}

	content := configMap.Data[dnsdomain.ZoneConfigMapKey("internal.example.test")]
	if content == "" {
		t.Fatal("expected rendered zone content")
	}
	if want := "app 300 IN A 192.0.2.10"; !strings.Contains(content, want) {
		t.Fatalf("expected rendered content %q in zone:\n%s", want, content)
	}
	if want := "api.portal 300 IN A 192.0.2.200"; !strings.Contains(content, want) {
		t.Fatalf("expected projected service content %q in zone:\n%s", want, content)
	}

	var updatedRecord dnsv1alpha1.DNSRecord
	if err := client.Get(context.Background(), types.NamespacedName{Name: record.Name, Namespace: record.Namespace}, &updatedRecord); err != nil {
		t.Fatalf("get updated record: %v", err)
	}
	if updatedRecord.Status.ZoneConfigMapName != dnsdomain.ZoneConfigMapName("internal.example.test") {
		t.Fatalf("expected zone configmap name %s, got %s", dnsdomain.ZoneConfigMapName("internal.example.test"), updatedRecord.Status.ZoneConfigMapName)
	}

	var updatedService publishv1alpha1.PublishedService
	if err := client.Get(context.Background(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, &updatedService); err != nil {
		t.Fatalf("get updated service: %v", err)
	}
	if updatedService.Status.URL != "https://api.portal.internal.example.test" {
		t.Fatalf("unexpected published service url: %s", updatedService.Status.URL)
	}
}

func TestReconcilePublishedServiceOnlyNamespaceSync(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	service := &publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "api",
			Namespace:  "dns-operator-system",
			Generation: 1,
		},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "api.internal.example.test",
			PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
			Backend: &publishv1alpha1.PublishBackend{
				Address: "192.0.2.20",
				Port:    8080,
			},
			TLS: &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
		},
	}
	runtimeService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dns-operator-caddy",
			Namespace: "dns-operator-system",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "192.0.2.200",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&publishv1alpha1.PublishedService{}).
		WithObjects(service, runtimeService).
		Build()

	reconciler := &DNSRecordReconciler{
		Client: client,
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: dnsdomain.ZoneSyncRequestName, Namespace: service.Namespace},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var configMap corev1.ConfigMap
	if err := client.Get(context.Background(), types.NamespacedName{
		Name:      dnsdomain.ZoneConfigMapName("internal.example.test"),
		Namespace: service.Namespace,
	}, &configMap); err != nil {
		t.Fatalf("get configmap: %v", err)
	}
}

func TestReconcileMarksExternalPublishedServiceAsNotAuthoritative(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	service := &publishv1alpha1.PublishedService{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "smoke",
			Namespace:  "dns-operator-system",
			Generation: 1,
		},
		Spec: publishv1alpha1.PublishedServiceSpec{
			Hostname:    "smoke.test.jerkytreats.dev",
			PublishMode: publishv1alpha1.PublishModeDNSOnly,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&publishv1alpha1.PublishedService{}).
		WithObjects(service).
		Build()

	reconciler := &DNSRecordReconciler{
		Client:     client,
		Scheme:     scheme,
		ZonePolicy: validation.MustNewZonePolicy([]string{"internal.example.test", "test.jerkytreats.dev"}, "internal.example.test"),
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: dnsdomain.ZoneSyncRequestName, Namespace: service.Namespace},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var updatedService publishv1alpha1.PublishedService
	if err := client.Get(context.Background(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, &updatedService); err != nil {
		t.Fatalf("get updated service: %v", err)
	}
	if got := conditionStatus(updatedService.Status.Conditions, common.ConditionDNSReady); got != metav1.ConditionTrue {
		t.Fatalf("expected DNSReady true for non-authoritative external host, got %#v", updatedService.Status.Conditions)
	}
}

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	if err := dnsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add dns scheme: %v", err)
	}
	if err := publishv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add publish scheme: %v", err)
	}
	return scheme
}

func conditionStatus(conditions []metav1.Condition, conditionType string) metav1.ConditionStatus {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return metav1.ConditionUnknown
}
