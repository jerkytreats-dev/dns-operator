package publish

import (
	"encoding/json"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	TailscaleExposeAnnotation = "tailscale.com/expose"
	TailscaleManagedLabel     = "tailscale.com/managed"
	TailscaleParentNameLabel  = "tailscale.com/parent-resource"
	TailscaleParentNSLabel    = "tailscale.com/parent-resource-ns"
	TailscaleParentTypeLabel  = "tailscale.com/parent-resource-type"
)

func ResolveRuntimeTarget(service *corev1.Service, proxySecret *corev1.Secret) string {
	if service == nil {
		return ""
	}

	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			return ingress.IP
		}
		if ingress.Hostname != "" {
			return ingress.Hostname
		}
	}

	if proxySecret != nil {
		if address := firstIPv4Address(proxySecret.Data["device_ips"]); address != "" {
			return address
		}
		if hostname := strings.TrimSuffix(string(proxySecret.Data["device_fqdn"]), "."); hostname != "" {
			return hostname
		}
	}

	if service.Spec.ClusterIP != "" && service.Spec.ClusterIP != corev1.ClusterIPNone {
		return service.Spec.ClusterIP
	}

	return ""
}

func firstIPv4Address(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}

	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return ""
	}

	for _, value := range values {
		ip := net.ParseIP(value)
		if ip == nil {
			continue
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String()
		}
	}

	if len(values) == 0 {
		return ""
	}
	return values[0]
}
