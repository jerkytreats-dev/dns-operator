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
				Address: "192.0.2.53",
			},
			Auth: tailscalev1alpha1.TailnetDNSAuth{
				SecretRef: &common.SecretKeyReference{Name: "tailscale-admin", Key: "api-key"},
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
		ClientFactory: func(tailnet string, auth tailnetdns.AuthConfig) (tailnetdns.SplitDNSClient, error) {
			if tailnet != "example.ts.net" || auth.APIToken != "tskey-api-123" {
				t.Fatalf("unexpected factory inputs: %s %#v", tailnet, auth)
			}
			return fakeSplitDNSClient{
				getResult: map[string][]string{
					"internal.example.test": {"192.0.2.53"},
				},
			}, nil
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

	if updated.Status.ConfiguredNameserver != "192.0.2.53" {
		t.Fatalf("unexpected configured nameserver: %s", updated.Status.ConfiguredNameserver)
	}
	if updated.Status.DriftDetected {
		t.Fatal("expected no drift")
	}
}

func TestTailnetDNSConfigReconcileOAuthSuccess(t *testing.T) {
	t.Parallel()

	scheme := newTailnetScheme(t)
	config := &tailscalev1alpha1.TailnetDNSConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-zone", Namespace: "dns-operator-system", Generation: 1},
		Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
			Zone:       "internal.example.test",
			Tailnet:    "example.ts.net",
			Nameserver: tailscalev1alpha1.TailnetNameserver{Address: "192.0.2.53"},
			Auth: tailscalev1alpha1.TailnetDNSAuth{
				OAuthClientCredentials: &tailscalev1alpha1.TailnetOAuthClientCredentials{
					ClientIDSecretRef:     common.SecretKeyReference{Name: "tailscale-oauth", Key: "client_id"},
					ClientSecretSecretRef: common.SecretKeyReference{Name: "tailscale-oauth", Key: "client_secret"},
				},
			},
			Behavior: tailscalev1alpha1.TailnetBehavior{Mode: tailscalev1alpha1.TailnetDNSBehaviorBootstrapAndRepair},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tailscale-oauth", Namespace: "dns-operator-system"},
		Data: map[string][]byte{
			"client_id":     []byte("oauth-client-id"),
			"client_secret": []byte("oauth-client-secret"),
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
		ClientFactory: func(tailnet string, auth tailnetdns.AuthConfig) (tailnetdns.SplitDNSClient, error) {
			if tailnet != "example.ts.net" {
				t.Fatalf("tailnet = %q, want example.ts.net", tailnet)
			}
			if auth.APIToken != "" || auth.OAuth == nil {
				t.Fatalf("expected oauth auth, got %#v", auth)
			}
			if auth.OAuth.ClientID != "oauth-client-id" || auth.OAuth.ClientSecret != "oauth-client-secret" {
				t.Fatalf("unexpected oauth credentials: %#v", auth)
			}
			if len(auth.OAuth.Scopes) != 1 || auth.OAuth.Scopes[0] != tailnetdns.DefaultOAuthScope {
				t.Fatalf("unexpected oauth scopes: %#v", auth.OAuth.Scopes)
			}
			return fakeSplitDNSClient{
				getResult: map[string][]string{
					"internal.example.test": {"192.0.2.53"},
				},
			}, nil
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
	if updated.Status.ConfiguredNameserver != "192.0.2.53" {
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
			Nameserver: tailscalev1alpha1.TailnetNameserver{Address: "192.0.2.53"},
			Auth:       tailscalev1alpha1.TailnetDNSAuth{SecretRef: &common.SecretKeyReference{Name: "missing", Key: "api-key"}},
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

func TestTailnetDNSConfigReconcileRejectsInvalidAuthModes(t *testing.T) {
	t.Parallel()

	tests := map[string]tailscalev1alpha1.TailnetDNSAuth{
		"empty": {},
		"mixed": {
			SecretRef: &common.SecretKeyReference{Name: "tailscale-admin", Key: "api-key"},
			OAuthClientCredentials: &tailscalev1alpha1.TailnetOAuthClientCredentials{
				ClientIDSecretRef:     common.SecretKeyReference{Name: "tailscale-oauth", Key: "client_id"},
				ClientSecretSecretRef: common.SecretKeyReference{Name: "tailscale-oauth", Key: "client_secret"},
			},
		},
		"unsupported scope": {
			OAuthClientCredentials: &tailscalev1alpha1.TailnetOAuthClientCredentials{
				ClientIDSecretRef:     common.SecretKeyReference{Name: "tailscale-oauth", Key: "client_id"},
				ClientSecretSecretRef: common.SecretKeyReference{Name: "tailscale-oauth", Key: "client_secret"},
				Scopes:                []string{"devices"},
			},
		},
		"cross namespace oauth": {
			OAuthClientCredentials: &tailscalev1alpha1.TailnetOAuthClientCredentials{
				ClientIDSecretRef:     common.SecretKeyReference{Name: "tailscale-oauth", Namespace: "shared-secrets", Key: "client_id"},
				ClientSecretSecretRef: common.SecretKeyReference{Name: "tailscale-oauth", Key: "client_secret"},
			},
		},
	}

	for name, auth := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scheme := newTailnetScheme(t)
			config := &tailscalev1alpha1.TailnetDNSConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "internal-zone", Namespace: "dns-operator-system", Generation: 1},
				Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
					Zone:       "internal.example.test",
					Tailnet:    "example.ts.net",
					Nameserver: tailscalev1alpha1.TailnetNameserver{Address: "192.0.2.53"},
					Auth:       auth,
					Behavior:   tailscalev1alpha1.TailnetBehavior{Mode: tailscalev1alpha1.TailnetDNSBehaviorBootstrapAndRepair},
				},
			}
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "tailscale-oauth", Namespace: "dns-operator-system"},
				Data: map[string][]byte{
					"client_id":     []byte("oauth-client-id"),
					"client_secret": []byte("oauth-client-secret"),
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
				ClientFactory: func(string, tailnetdns.AuthConfig) (tailnetdns.SplitDNSClient, error) {
					t.Fatal("client factory should not be called for invalid auth")
					return nil, nil
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
			if !updated.Status.DriftDetected {
				t.Fatal("expected drift to remain true for invalid auth")
			}
			if got := conditionStatus(updated.Status.Conditions, common.ConditionCredentialsReady); got != metav1.ConditionFalse {
				t.Fatalf("CredentialsReady = %s, want False", got)
			}
		})
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
			Nameserver: tailscalev1alpha1.TailnetNameserver{Address: "192.0.2.53"},
			Auth:       tailscalev1alpha1.TailnetDNSAuth{SecretRef: &common.SecretKeyReference{Name: "tailscale-admin", Key: "api-key"}},
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
		ClientFactory: func(string, tailnetdns.AuthConfig) (tailnetdns.SplitDNSClient, error) {
			return failingSplitDNSClient{}, nil
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

func TestTailnetDNSConfigReconcileRejectsCrossNamespaceSecretRef(t *testing.T) {
	t.Parallel()

	scheme := newTailnetScheme(t)
	config := &tailscalev1alpha1.TailnetDNSConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-zone", Namespace: "dns-operator-system", Generation: 1},
		Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
			Zone:       "internal.example.test",
			Tailnet:    "example.ts.net",
			Nameserver: tailscalev1alpha1.TailnetNameserver{Address: "192.0.2.53"},
			Auth: tailscalev1alpha1.TailnetDNSAuth{
				SecretRef: &common.SecretKeyReference{Name: "tailscale-admin", Namespace: "shared-secrets", Key: "api-key"},
			},
			Behavior: tailscalev1alpha1.TailnetBehavior{Mode: tailscalev1alpha1.TailnetDNSBehaviorBootstrapAndRepair},
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
		t.Fatal("expected drift to remain true for rejected cross-namespace secret refs")
	}
}

func TestTailnetDNSConfigReconcileResolvesEndpointRef(t *testing.T) {
	t.Parallel()

	scheme := newTailnetScheme(t)
	config := &tailscalev1alpha1.TailnetDNSConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-zone", Namespace: "dns-operator-system", Generation: 1},
		Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
			Zone:    "internal.example.test",
			Tailnet: "example.ts.net",
			Nameserver: tailscalev1alpha1.TailnetNameserver{
				EndpointRef: &common.ObjectReference{Name: "internal-authority"},
			},
			Auth: tailscalev1alpha1.TailnetDNSAuth{
				SecretRef: &common.SecretKeyReference{Name: "tailscale-admin", Key: "api-key"},
			},
			Behavior: tailscalev1alpha1.TailnetBehavior{Mode: tailscalev1alpha1.TailnetDNSBehaviorBootstrapAndRepair},
		},
	}
	endpoint := &tailscalev1alpha1.TailnetDNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-authority", Namespace: "dns-operator-system"},
		Status: tailscalev1alpha1.TailnetDNSEndpointStatus{
			EndpointAddress: "100.100.100.100",
			Conditions:      []metav1.Condition{{Type: common.ConditionReady, Status: metav1.ConditionTrue}},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tailscale-admin", Namespace: "dns-operator-system"},
		Data:       map[string][]byte{"api-key": []byte("tskey-api-123")},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tailscalev1alpha1.TailnetDNSConfig{}).
		WithObjects(config, endpoint, secret).
		Build()

	reconciler := &TailnetDNSConfigReconciler{
		Client: client,
		Scheme: scheme,
		ClientFactory: func(string, tailnetdns.AuthConfig) (tailnetdns.SplitDNSClient, error) {
			return &fakeSplitDNSClient{getResult: map[string][]string{}, patchResult: map[string][]string{"internal.example.test": {"100.100.100.100"}}}, nil
		},
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"}}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var updated tailscalev1alpha1.TailnetDNSConfig
	if err := client.Get(context.Background(), types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"}, &updated); err != nil {
		t.Fatalf("get updated object: %v", err)
	}
	if updated.Status.ConfiguredNameserver != "100.100.100.100" {
		t.Fatalf("unexpected configured nameserver: %s", updated.Status.ConfiguredNameserver)
	}
}

func TestTailnetDNSConfigReconcileRejectsUnreadyEndpointRef(t *testing.T) {
	t.Parallel()

	scheme := newTailnetScheme(t)
	config := &tailscalev1alpha1.TailnetDNSConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-zone", Namespace: "dns-operator-system", Generation: 1},
		Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
			Zone:    "internal.example.test",
			Tailnet: "example.ts.net",
			Nameserver: tailscalev1alpha1.TailnetNameserver{
				EndpointRef: &common.ObjectReference{Name: "internal-authority"},
			},
			Auth: tailscalev1alpha1.TailnetDNSAuth{
				SecretRef: &common.SecretKeyReference{Name: "tailscale-admin", Key: "api-key"},
			},
			Behavior: tailscalev1alpha1.TailnetBehavior{Mode: tailscalev1alpha1.TailnetDNSBehaviorBootstrapAndRepair},
		},
	}
	endpoint := &tailscalev1alpha1.TailnetDNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-authority", Namespace: "dns-operator-system"},
		Status:     tailscalev1alpha1.TailnetDNSEndpointStatus{},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tailscale-admin", Namespace: "dns-operator-system"},
		Data:       map[string][]byte{"api-key": []byte("tskey-api-123")},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tailscalev1alpha1.TailnetDNSConfig{}).
		WithObjects(config, endpoint, secret).
		Build()

	reconciler := &TailnetDNSConfigReconciler{Client: client, Scheme: scheme}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"}}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var updated tailscalev1alpha1.TailnetDNSConfig
	if err := client.Get(context.Background(), types.NamespacedName{Name: "internal-zone", Namespace: "dns-operator-system"}, &updated); err != nil {
		t.Fatalf("get updated object: %v", err)
	}
	if updated.Status.ConfiguredNameserver != "" {
		t.Fatalf("expected no configured nameserver, got %s", updated.Status.ConfiguredNameserver)
	}
	if !updated.Status.DriftDetected {
		t.Fatal("expected drift to remain detected when endpoint is not ready")
	}
}

type failingSplitDNSClient struct{}

func (failingSplitDNSClient) GetSplitDNS(context.Context) (map[string][]string, error) {
	return nil, errors.New("boom")
}

func (failingSplitDNSClient) PatchSplitDNS(context.Context, map[string]any) (map[string][]string, error) {
	return nil, errors.New("boom")
}

func conditionStatus(conditions []metav1.Condition, conditionType string) metav1.ConditionStatus {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return metav1.ConditionUnknown
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
