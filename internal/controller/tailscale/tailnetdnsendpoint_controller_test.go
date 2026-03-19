package tailscale

import (
	"context"
	"testing"

	"github.com/jerkytreats/dns-operator/api/common"
	tailscalev1alpha1 "github.com/jerkytreats/dns-operator/api/tailscale/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTailnetDNSEndpointReconcileCreatesExposureService(t *testing.T) {
	t.Parallel()

	scheme := newTailnetScheme(t)
	endpoint := &tailscalev1alpha1.TailnetDNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-authority", Namespace: "dns-operator-system", Generation: 1},
		Spec: tailscalev1alpha1.TailnetDNSEndpointSpec{
			Zone:    "internal.example.test",
			Tailnet: "example.ts.net",
			Service: tailscalev1alpha1.TailnetDNSEndpointService{Ref: common.ObjectReference{Name: "dns-operator-authoritative-dns"}},
			Auth:    tailscalev1alpha1.TailnetDNSEndpointAuth{SecretRef: common.SecretKeyReference{Name: "tailscale-admin", Key: "api-key"}},
			Exposure: tailscalev1alpha1.TailnetDNSEndpointExposure{
				Mode:     tailscalev1alpha1.TailnetDNSEndpointExposureModeVIPService,
				Hostname: "internal-authority",
			},
		},
	}
	targetService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "dns-operator-authoritative-dns", Namespace: "dns-operator-system"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.43.0.10",
			Selector: map[string]string{
				"app.kubernetes.io/name":      "dns-operator",
				"app.kubernetes.io/component": "coredns",
			},
			Ports: []corev1.ServicePort{
				{Name: "dns-tcp", Port: 53, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromString("dns-tcp")},
				{Name: "dns-udp", Port: 53, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromString("dns-udp")},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tailscale-admin", Namespace: "dns-operator-system"},
		Data:       map[string][]byte{"api-key": []byte("tskey-api-123")},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tailscalev1alpha1.TailnetDNSEndpoint{}).
		WithObjects(endpoint, targetService, secret).
		Build()

	reconciler := &TailnetDNSEndpointReconciler{Client: client, Scheme: scheme}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: endpoint.Name, Namespace: endpoint.Namespace}}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var exposureService corev1.Service
	if err := client.Get(context.Background(), types.NamespacedName{Name: "internal-authority-tailscale", Namespace: endpoint.Namespace}, &exposureService); err != nil {
		t.Fatalf("get exposure service: %v", err)
	}
	if exposureService.Annotations["tailscale.com/expose"] != "true" {
		t.Fatalf("expected tailscale expose annotation, got %#v", exposureService.Annotations)
	}

	var updated tailscalev1alpha1.TailnetDNSEndpoint
	if err := client.Get(context.Background(), types.NamespacedName{Name: endpoint.Name, Namespace: endpoint.Namespace}, &updated); err != nil {
		t.Fatalf("get updated endpoint: %v", err)
	}
	if updated.Status.ExposureServiceRef == nil || updated.Status.ExposureServiceRef.Name != "internal-authority-tailscale" {
		t.Fatalf("unexpected exposure service ref: %#v", updated.Status.ExposureServiceRef)
	}
	ready := apiMeta.FindStatusCondition(updated.Status.Conditions, common.ConditionEndpointReady)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("expected endpoint to remain pending until VIP allocation, got %#v", ready)
	}
}

func TestTailnetDNSEndpointReconcileReportsReadyEndpoint(t *testing.T) {
	t.Parallel()

	scheme := newTailnetScheme(t)
	endpoint := &tailscalev1alpha1.TailnetDNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-authority", Namespace: "dns-operator-system", Generation: 1},
		Spec: tailscalev1alpha1.TailnetDNSEndpointSpec{
			Zone:    "internal.example.test",
			Tailnet: "example.ts.net",
			Service: tailscalev1alpha1.TailnetDNSEndpointService{Ref: common.ObjectReference{Name: "dns-operator-authoritative-dns"}},
			Auth:    tailscalev1alpha1.TailnetDNSEndpointAuth{SecretRef: common.SecretKeyReference{Name: "tailscale-admin", Key: "api-key"}},
			Exposure: tailscalev1alpha1.TailnetDNSEndpointExposure{
				Mode:     tailscalev1alpha1.TailnetDNSEndpointExposureModeVIPService,
				Hostname: "internal-authority",
			},
		},
	}
	targetService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "dns-operator-authoritative-dns", Namespace: "dns-operator-system"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.43.0.10",
			Selector:  map[string]string{"app": "coredns"},
			Ports: []corev1.ServicePort{
				{Name: "dns-tcp", Port: 53, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromString("dns-tcp")},
				{Name: "dns-udp", Port: 53, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromString("dns-udp")},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tailscale-admin", Namespace: "dns-operator-system"},
		Data:       map[string][]byte{"api-key": []byte("tskey-api-123")},
	}
	exposureService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "internal-authority-tailscale",
			Namespace: "dns-operator-system",
			Annotations: map[string]string{
				"tailscale.com/expose":   "true",
				"tailscale.com/hostname": "internal-authority",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.43.0.20",
			Selector:  map[string]string{"app": "coredns"},
			Ports: []corev1.ServicePort{
				{Name: "dns-tcp", Port: 53, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromString("dns-tcp")},
				{Name: "dns-udp", Port: 53, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromString("dns-udp")},
			},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{Hostname: "internal-authority.example.ts.net", IP: "100.100.100.100"}}}},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tailscalev1alpha1.TailnetDNSEndpoint{}).
		WithObjects(endpoint, targetService, secret, exposureService).
		Build()

	reconciler := &TailnetDNSEndpointReconciler{Client: client, Scheme: scheme}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: endpoint.Name, Namespace: endpoint.Namespace}}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var updated tailscalev1alpha1.TailnetDNSEndpoint
	if err := client.Get(context.Background(), types.NamespacedName{Name: endpoint.Name, Namespace: endpoint.Namespace}, &updated); err != nil {
		t.Fatalf("get updated endpoint: %v", err)
	}
	if updated.Status.EndpointAddress != "100.100.100.100" {
		t.Fatalf("unexpected endpoint address: %s", updated.Status.EndpointAddress)
	}
	ready := apiMeta.FindStatusCondition(updated.Status.Conditions, common.ConditionEndpointReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		t.Fatalf("expected ready endpoint condition, got %#v", ready)
	}
}
