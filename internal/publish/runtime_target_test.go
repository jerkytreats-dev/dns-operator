package publish

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResolveRuntimeTargetPrefersLoadBalancerIngress(t *testing.T) {
	t.Parallel()

	service := &corev1.Service{
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{
					IP: "198.51.100.20",
				}},
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.43.0.20",
		},
	}

	if got := ResolveRuntimeTarget(service, nil); got != "198.51.100.20" {
		t.Fatalf("unexpected runtime target: %s", got)
	}
}

func TestResolveRuntimeTargetReadsProxySecret(t *testing.T) {
	t.Parallel()

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RuntimeServiceName,
			Namespace: "dns-operator-system",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.43.0.20",
		},
	}
	proxySecret := &corev1.Secret{
		Data: map[string][]byte{
			"device_fqdn": []byte("internal-web.tail1cfaab.ts.net."),
			"device_ips":  []byte(`["fd7a:115c:a1e0::1","100.120.77.22"]`),
		},
	}

	if got := ResolveRuntimeTarget(service, proxySecret); got != "100.120.77.22" {
		t.Fatalf("unexpected runtime target: %s", got)
	}
}

func TestResolveRuntimeTargetFallsBackToClusterIP(t *testing.T) {
	t.Parallel()

	service := &corev1.Service{
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.43.0.20",
		},
	}

	if got := ResolveRuntimeTarget(service, nil); got != "10.43.0.20" {
		t.Fatalf("unexpected runtime target: %s", got)
	}
}
