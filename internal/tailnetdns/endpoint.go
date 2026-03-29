package tailnetdns

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/jerkytreats/dns-operator/api/common"
	tailscalev1alpha1 "github.com/jerkytreats/dns-operator/api/tailscale/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	TailscaleExposeAnnotation   = "tailscale.com/expose"
	TailscaleHostnameAnnotation = "tailscale.com/hostname"
	TailscaleManagedLabel       = "tailscale.com/managed"
	TailscaleParentNameLabel    = "tailscale.com/parent-resource"
	TailscaleParentNSLabel      = "tailscale.com/parent-resource-ns"
	TailscaleParentTypeLabel    = "tailscale.com/parent-resource-type"

	EndpointComponentLabel = "tailnet-dns-endpoint"
)

type ExposureStatus struct {
	EndpointHostname string
	EndpointDNSName  string
	EndpointAddress  string
	Ready            bool
}

func ExposureServiceName(endpointName string) string {
	return endpointName + "-tailscale"
}

func BuildExposureService(endpoint *tailscalev1alpha1.TailnetDNSEndpoint, target *corev1.Service) (*corev1.Service, error) {
	if endpoint == nil {
		return nil, fmt.Errorf("endpoint is required")
	}
	if target == nil {
		return nil, fmt.Errorf("target service is required")
	}
	if len(target.Spec.Selector) == 0 {
		return nil, fmt.Errorf("target service %s/%s must define a selector", target.Namespace, target.Name)
	}
	if target.Spec.ClusterIP == "" || target.Spec.ClusterIP == corev1.ClusterIPNone {
		return nil, fmt.Errorf("target service %s/%s must have a cluster IP", target.Namespace, target.Name)
	}

	ports, err := dnsPorts(target.Spec.Ports)
	if err != nil {
		return nil, fmt.Errorf("target service %s/%s %w", target.Namespace, target.Name, err)
	}

	selector := make(map[string]string, len(target.Spec.Selector))
	for key, value := range target.Spec.Selector {
		selector[key] = value
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ExposureServiceName(endpoint.Name),
			Namespace: endpoint.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "dns-operator",
				"app.kubernetes.io/component": EndpointComponentLabel,
			},
			Annotations: map[string]string{
				TailscaleExposeAnnotation:   "true",
				TailscaleHostnameAnnotation: endpoint.Spec.Exposure.Hostname,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			Selector:                 selector,
			Ports:                    ports,
			PublishNotReadyAddresses: target.Spec.PublishNotReadyAddresses,
		},
	}, nil
}

func ObserveExposureService(service *corev1.Service, proxySecret *corev1.Secret) ExposureStatus {
	if service == nil {
		return ExposureStatus{}
	}

	status := ExposureStatus{
		EndpointHostname: service.Annotations[TailscaleHostnameAnnotation],
	}

	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if status.EndpointDNSName == "" && ingress.Hostname != "" {
			status.EndpointDNSName = ingress.Hostname
		}
		if status.EndpointAddress == "" && ingress.IP != "" {
			status.EndpointAddress = ingress.IP
		}
	}

	if proxySecret != nil {
		if status.EndpointDNSName == "" {
			status.EndpointDNSName = strings.TrimSuffix(string(proxySecret.Data["device_fqdn"]), ".")
		}
		if status.EndpointAddress == "" {
			status.EndpointAddress = firstIPv4(proxySecret.Data["device_ips"])
		}
	}

	status.Ready = status.EndpointAddress != ""
	return status
}

func firstIPv4(raw []byte) string {
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

func ResolveNameserverAddress(config *tailscalev1alpha1.TailnetDNSConfig, endpoint *tailscalev1alpha1.TailnetDNSEndpoint) (string, error) {
	if config == nil {
		return "", fmt.Errorf("tailnet dns config is required")
	}
	if config.Spec.Nameserver.Address != "" {
		return config.Spec.Nameserver.Address, nil
	}
	if config.Spec.Nameserver.EndpointRef == nil {
		return "", fmt.Errorf("nameserver must define either address or endpointRef")
	}
	if endpoint == nil {
		return "", fmt.Errorf("referenced tailnet dns endpoint was not found")
	}
	ready := apiMeta.FindStatusCondition(endpoint.Status.Conditions, common.ConditionReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return "", fmt.Errorf("referenced tailnet dns endpoint %s/%s is not ready", endpoint.Namespace, endpoint.Name)
	}
	if endpoint.Status.EndpointAddress == "" {
		return "", fmt.Errorf("referenced tailnet dns endpoint %s/%s does not report an endpoint address", endpoint.Namespace, endpoint.Name)
	}
	return endpoint.Status.EndpointAddress, nil
}

func dnsPorts(ports []corev1.ServicePort) ([]corev1.ServicePort, error) {
	var tcpPort *corev1.ServicePort
	var udpPort *corev1.ServicePort

	for index := range ports {
		port := ports[index]
		if port.Port != 53 {
			continue
		}
		switch port.Protocol {
		case corev1.ProtocolTCP:
			copied := sanitizeDNSPort(port)
			tcpPort = &copied
		case corev1.ProtocolUDP:
			copied := sanitizeDNSPort(port)
			udpPort = &copied
		}
	}

	if tcpPort == nil || udpPort == nil {
		return nil, fmt.Errorf("must expose both TCP 53 and UDP 53")
	}

	return []corev1.ServicePort{*tcpPort, *udpPort}, nil
}

func sanitizeDNSPort(port corev1.ServicePort) corev1.ServicePort {
	return corev1.ServicePort{
		Name:        port.Name,
		Protocol:    port.Protocol,
		AppProtocol: port.AppProtocol,
		Port:        port.Port,
		TargetPort:  normalizeTargetPort(port),
	}
}

func normalizeTargetPort(port corev1.ServicePort) intstr.IntOrString {
	if port.TargetPort.Type == intstr.Int && port.TargetPort.IntValue() == 0 {
		return intstr.FromInt32(port.Port)
	}
	if port.TargetPort.Type == intstr.String && port.TargetPort.StrVal == "" {
		return intstr.FromInt32(port.Port)
	}
	return port.TargetPort
}
