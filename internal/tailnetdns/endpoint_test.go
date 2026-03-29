package tailnetdns

import (
	"testing"

	"github.com/jerkytreats/dns-operator/api/common"
	tailscalev1alpha1 "github.com/jerkytreats/dns-operator/api/tailscale/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const testEndpointHostname = "internal-authority"

func TestBuildExposureServiceMirrorsDNSPortsAndSelector(t *testing.T) {
	t.Parallel()

	endpoint := &tailscalev1alpha1.TailnetDNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-authority", Namespace: "dns-operator-system"},
		Spec: tailscalev1alpha1.TailnetDNSEndpointSpec{
			Exposure: tailscalev1alpha1.TailnetDNSEndpointExposure{Hostname: testEndpointHostname},
		},
	}
	target := &corev1.Service{
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
				{Name: "health", Port: 8080, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(8080)},
			},
		},
	}

	rendered, err := BuildExposureService(endpoint, target)
	if err != nil {
		t.Fatalf("build exposure service: %v", err)
	}
	if rendered.Name != "internal-authority-tailscale" {
		t.Fatalf("unexpected service name: %s", rendered.Name)
	}
	if rendered.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("unexpected service type: %s", rendered.Spec.Type)
	}
	if rendered.Annotations[TailscaleExposeAnnotation] != "true" {
		t.Fatalf("expected Tailscale expose annotation, got %#v", rendered.Annotations)
	}
	if rendered.Annotations[TailscaleHostnameAnnotation] != testEndpointHostname {
		t.Fatalf("unexpected hostname annotation: %s", rendered.Annotations[TailscaleHostnameAnnotation])
	}
	if len(rendered.Spec.Ports) != 2 {
		t.Fatalf("expected only dns ports, got %#v", rendered.Spec.Ports)
	}
	if rendered.Spec.Selector["app.kubernetes.io/component"] != "coredns" {
		t.Fatalf("selector was not mirrored: %#v", rendered.Spec.Selector)
	}
}

func TestBuildExposureServiceRejectsMissingDNSPorts(t *testing.T) {
	t.Parallel()

	endpoint := &tailscalev1alpha1.TailnetDNSEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "internal-authority", Namespace: "dns-operator-system"}}
	target := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "dns-operator-authoritative-dns", Namespace: "dns-operator-system"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.43.0.10",
			Selector:  map[string]string{"app": "coredns"},
			Ports: []corev1.ServicePort{
				{Name: "dns-tcp", Port: 53, Protocol: corev1.ProtocolTCP},
			},
		},
	}

	if _, err := BuildExposureService(endpoint, target); err == nil {
		t.Fatal("expected missing udp port to be rejected")
	}
}

func TestObserveExposureServiceReadsVIPStatus(t *testing.T) {
	t.Parallel()

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "internal-authority-tailscale",
			Namespace: "dns-operator-system",
			Annotations: map[string]string{
				TailscaleHostnameAnnotation: testEndpointHostname,
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{Hostname: "internal-authority.example.ts.net", IP: "100.100.100.100"}},
			},
		},
	}

	status := ObserveExposureService(service, nil)
	if !status.Ready {
		t.Fatal("expected exposure status to be ready")
	}
	if status.EndpointAddress != "100.100.100.100" {
		t.Fatalf("unexpected endpoint address: %s", status.EndpointAddress)
	}
	if status.EndpointDNSName != "internal-authority.example.ts.net" {
		t.Fatalf("unexpected endpoint dns name: %s", status.EndpointDNSName)
	}
	if status.EndpointHostname != testEndpointHostname {
		t.Fatalf("unexpected endpoint hostname: %s", status.EndpointHostname)
	}
}

func TestObserveExposureServiceReadsProxySecretStatus(t *testing.T) {
	t.Parallel()

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "internal-authority-tailscale",
			Namespace: "dns-operator-system",
			Annotations: map[string]string{
				TailscaleHostnameAnnotation: testEndpointHostname,
			},
		},
	}
	proxySecret := &corev1.Secret{
		Data: map[string][]byte{
			"device_fqdn": []byte("internal-authority.example.ts.net."),
			"device_ips":  []byte(`["fd7a:115c:a1e0::1","100.114.159.106"]`),
		},
	}

	status := ObserveExposureService(service, proxySecret)
	if !status.Ready {
		t.Fatal("expected exposure status to be ready")
	}
	if status.EndpointAddress != "100.114.159.106" {
		t.Fatalf("unexpected endpoint address: %s", status.EndpointAddress)
	}
	if status.EndpointDNSName != "internal-authority.example.ts.net" {
		t.Fatalf("unexpected endpoint dns name: %s", status.EndpointDNSName)
	}
	if status.EndpointHostname != testEndpointHostname {
		t.Fatalf("unexpected endpoint hostname: %s", status.EndpointHostname)
	}
}

func TestResolveNameserverAddressFromEndpoint(t *testing.T) {
	t.Parallel()

	config := &tailscalev1alpha1.TailnetDNSConfig{
		Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
			Nameserver: tailscalev1alpha1.TailnetNameserver{
				EndpointRef: &common.ObjectReference{Name: "internal-authority"},
			},
		},
	}
	endpoint := &tailscalev1alpha1.TailnetDNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-authority", Namespace: "dns-operator-system"},
		Status: tailscalev1alpha1.TailnetDNSEndpointStatus{
			EndpointAddress: "100.100.100.100",
			Conditions:      []metav1.Condition{{Type: common.ConditionReady, Status: metav1.ConditionTrue}},
		},
	}

	address, err := ResolveNameserverAddress(config, endpoint)
	if err != nil {
		t.Fatalf("resolve nameserver address: %v", err)
	}
	if address != "100.100.100.100" {
		t.Fatalf("unexpected resolved address: %s", address)
	}
}
