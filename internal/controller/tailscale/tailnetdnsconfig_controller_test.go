package tailscale

import (
	"context"
	"errors"
	"testing"

	"github.com/jerkytreats/dns-operator/api/common"
	tailscalev1alpha1 "github.com/jerkytreats/dns-operator/api/tailscale/v1alpha1"
	"github.com/jerkytreats/dns-operator/internal/tailnetdns"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fakeSplitDNSClient struct {
	getResult   map[string][]string
	patchResult map[string][]string
	getErr      error
	patchErr    error
}

func (f fakeSplitDNSClient) GetSplitDNS(context.Context) (map[string][]string, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getResult, nil
}

func (f fakeSplitDNSClient) PatchSplitDNS(context.Context, map[string]any) (map[string][]string, error) {
	if f.patchErr != nil {
		return nil, f.patchErr
	}
	return f.patchResult, nil
}

func TestTailnetDNSConfigReconcileSuccess(t *testing.T) {
	t.Parallel()

	scheme := newTailnetScheme(t)
	config := &tailscalev1alpha1.TailnetDNSConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-zone", Namespace: "dns-operator-system", Generation: 1},
		Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
			Zone:    "internal.example.test",
			Tailnet: "example.ts.net",
			Nameserver: tailscalev1alpha1.TailnetNameserver{
				Address: "100.70.110.111",
			},
			Auth: tailscalev1alpha1.TailnetDNSAuth{
				SecretRef: common.SecretKeyReference{Name: "tailscale-admin", Key: "api-key"},
			},
			Behavior: tailscalev1alpha1.TailnetBehavior{Mode: tailscalev1alpha1.TailnetDNSBehaviorBootstrapAndRepair},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tailscale-admin", Namespace: "dns-operator-system"},
		Data: map[string][]byte{
			"api-key": []byte("tskey-api-123"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tailscalev1alpha1.TailnetDNSConfig{}).
		WithObjects(config, secret).
		Build()

	reconciler := &TailnetDNSConfigReconciler{
		Client: client,
		Scheme: scheme,
		ClientFactory: func(tailnet, token string) tailnetdns.SplitDNSClient {
			if tailnet != "example.ts.net" || token != "tskey-api-123" {
				t.Fatalf("unexpected factory inputs: %s %s", tailnet, token)
			}
			return fakeSplitDNSClient{
				getResult: map[string][]string{
					"internal.example.test": {"100.70.110.111"},
				},
			}
		},
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var updated tailscalev1alpha1.TailnetDNSConfig
	if err := client.Get(context.Background(), types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"}, &updated); err != nil {
		t.Fatalf("get updated object: %v", err)
	}

	if updated.Status.ConfiguredNameserver != "100.70.110.111" {
		t.Fatalf("unexpected configured nameserver: %s", updated.Status.ConfiguredNameserver)
	}
	if updated.Status.DriftDetected {
		t.Fatal("expected no drift")
	}
}

func TestTailnetDNSConfigReconcileMissingSecret(t *testing.T) {
	t.Parallel()

	scheme := newTailnetScheme(t)
	config := &tailscalev1alpha1.TailnetDNSConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-zone", Namespace: "dns-operator-system", Generation: 1},
		Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
			Zone:       "internal.example.test",
			Tailnet:    "example.ts.net",
			Nameserver: tailscalev1alpha1.TailnetNameserver{Address: "100.70.110.111"},
			Auth:       tailscalev1alpha1.TailnetDNSAuth{SecretRef: common.SecretKeyReference{Name: "missing", Key: "api-key"}},
			Behavior:   tailscalev1alpha1.TailnetBehavior{Mode: tailscalev1alpha1.TailnetDNSBehaviorBootstrapAndRepair},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tailscalev1alpha1.TailnetDNSConfig{}).
		WithObjects(config).
		Build()

	reconciler := &TailnetDNSConfigReconciler{Client: client, Scheme: scheme}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var updated tailscalev1alpha1.TailnetDNSConfig
	if err := client.Get(context.Background(), types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"}, &updated); err != nil {
		t.Fatalf("get updated object: %v", err)
	}
	if !updated.Status.DriftDetected {
		t.Fatal("expected drift to remain true when credentials are missing")
	}
}

func TestTailnetDNSConfigReconcileApplyFailure(t *testing.T) {
	t.Parallel()

	scheme := newTailnetScheme(t)
	config := &tailscalev1alpha1.TailnetDNSConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-zone", Namespace: "dns-operator-system", Generation: 1},
		Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
			Zone:       "internal.example.test",
			Tailnet:    "example.ts.net",
			Nameserver: tailscalev1alpha1.TailnetNameserver{Address: "100.70.110.111"},
			Auth:       tailscalev1alpha1.TailnetDNSAuth{SecretRef: common.SecretKeyReference{Name: "tailscale-admin", Key: "api-key"}},
			Behavior:   tailscalev1alpha1.TailnetBehavior{Mode: tailscalev1alpha1.TailnetDNSBehaviorBootstrapAndRepair},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tailscale-admin", Namespace: "dns-operator-system"},
		Data: map[string][]byte{
			"api-key": []byte("tskey-api-123"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tailscalev1alpha1.TailnetDNSConfig{}).
		WithObjects(config, secret).
		Build()

	reconciler := &TailnetDNSConfigReconciler{
		Client: client,
		Scheme: scheme,
		ClientFactory: func(string, string) tailnetdns.SplitDNSClient {
			return failingSplitDNSClient{}
		},
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"},
	}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var updated tailscalev1alpha1.TailnetDNSConfig
	if err := client.Get(context.Background(), types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"}, &updated); err != nil {
		t.Fatalf("get updated object: %v", err)
	}
	if updated.Status.ConfiguredNameserver != "" {
		t.Fatalf("expected no configured nameserver on failure, got %s", updated.Status.ConfiguredNameserver)
	}
	if !updated.Status.DriftDetected {
		t.Fatal("expected drift to remain detected on failure")
	}
}

type failingSplitDNSClient struct{}

func (failingSplitDNSClient) GetSplitDNS(context.Context) (map[string][]string, error) {
	return nil, errors.New("boom")
}

func (failingSplitDNSClient) PatchSplitDNS(context.Context, map[string]any) (map[string][]string, error) {
	return nil, errors.New("boom")
}

func newTailnetScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	if err := tailscalev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add tailscale scheme: %v", err)
	}
	return scheme
}
