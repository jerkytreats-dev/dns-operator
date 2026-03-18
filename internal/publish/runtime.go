package publish

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	"github.com/jerkytreats/dns-operator/api/common"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

const (
	RuntimeConfigMapName          = "caddy-runtime"
	RuntimeConfigMapKey           = "runtime.caddy"
	RuntimeCertificatesSecretName = "caddy-runtime-certificates"
	RuntimeServiceName            = "dns-operator-caddy"
	runtimeCertificateMountPath   = "/etc/dns-operator/certs"
	ReasonValidationFailed        = "ValidationFailed"
	ReasonReferencesUnresolved    = "ReferencesUnresolved"
)

type RenderedRuntime struct {
	ConfigMapName          string
	ConfigMapKey           string
	CertificatesSecretName string
	Content                string
	Hash                   string
	CertificateSecretData  map[string][]byte
}

type ServiceRuntimeStatus struct {
	URL                  string
	RuntimeRequired      bool
	RenderedConfigMap    string
	CertificateBundleRef *common.ObjectReference
	CertificateSecretRef *common.ObjectReference
	Reason               string
	Err                  error
}

type runtimeSite struct {
	Hostname            string
	BackendAddress      string
	BackendPort         int32
	BackendProtocol     string
	InsecureSkipVerify  bool
	CertificateCertFile string
	CertificateKeyFile  string
}

func BuildRuntime(
	services []publishv1alpha1.PublishedService,
	bundles []certificatev1alpha1.CertificateBundle,
	bundleSecrets map[types.NamespacedName]*corev1.Secret,
	hostnameValidator func(string) error,
) (RenderedRuntime, map[types.NamespacedName]ServiceRuntimeStatus, error) {
	statuses := make(map[types.NamespacedName]ServiceRuntimeStatus, len(services))
	sites := make([]runtimeSite, 0, len(services))
	certificateData := map[string][]byte{}

	sortedServices := append([]publishv1alpha1.PublishedService(nil), services...)
	sort.Slice(sortedServices, func(i, j int) bool {
		if sortedServices[i].Namespace == sortedServices[j].Namespace {
			return sortedServices[i].Name < sortedServices[j].Name
		}
		return sortedServices[i].Namespace < sortedServices[j].Namespace
	})

	for _, service := range sortedServices {
		key := types.NamespacedName{Name: service.Name, Namespace: service.Namespace}
		status := ServiceRuntimeStatus{
			RuntimeRequired:   service.Spec.PublishMode == publishv1alpha1.PublishModeHTTPSProxy,
			RenderedConfigMap: RuntimeConfigMapName,
		}
		if service.Spec.PublishMode == publishv1alpha1.PublishModeHTTPSProxy {
			status.URL = "https://" + service.Spec.Hostname
		}

		if hostnameValidator != nil {
			if err := hostnameValidator(service.Spec.Hostname); err != nil {
				status.Reason = ReasonValidationFailed
				status.Err = err
				statuses[key] = status
				continue
			}
		}

		if service.Spec.PublishMode != publishv1alpha1.PublishModeHTTPSProxy {
			statuses[key] = status
			continue
		}

		site, bundleRef, secretRef, reason, err := siteForService(service, bundles, bundleSecrets)
		status.CertificateBundleRef = bundleRef
		status.CertificateSecretRef = secretRef
		status.Reason = reason
		status.Err = err
		statuses[key] = status
		if err != nil {
			continue
		}

		sites = append(sites, site)

		secretKey := types.NamespacedName{Name: secretRef.Name, Namespace: secretRef.Namespace}
		secret := bundleSecrets[secretKey]
		certFile, keyFile := certificateFileNames(secret.Name)
		certificateData[certFile] = append([]byte(nil), secret.Data[corev1.TLSCertKey]...)
		certificateData[keyFile] = append([]byte(nil), secret.Data[corev1.TLSPrivateKeyKey]...)
	}

	sort.Slice(sites, func(i, j int) bool {
		return sites[i].Hostname < sites[j].Hostname
	})

	content := renderCaddyfile(sites)
	return RenderedRuntime{
		ConfigMapName:          RuntimeConfigMapName,
		ConfigMapKey:           RuntimeConfigMapKey,
		CertificatesSecretName: RuntimeCertificatesSecretName,
		Content:                content,
		Hash:                   sha1Hex(content),
		CertificateSecretData:  certificateData,
	}, statuses, nil
}

func siteForService(
	service publishv1alpha1.PublishedService,
	bundles []certificatev1alpha1.CertificateBundle,
	bundleSecrets map[types.NamespacedName]*corev1.Secret,
) (runtimeSite, *common.ObjectReference, *common.ObjectReference, string, error) {
	if service.Spec.Backend == nil {
		return runtimeSite{}, nil, nil, ReasonValidationFailed, fmt.Errorf("backend is required for httpsProxy services")
	}
	if service.Spec.Backend.Address == "" {
		return runtimeSite{}, nil, nil, ReasonValidationFailed, fmt.Errorf("backend.address is required for httpsProxy services")
	}
	if service.Spec.Backend.Port == 0 {
		return runtimeSite{}, nil, nil, ReasonValidationFailed, fmt.Errorf("backend.port is required for httpsProxy services")
	}
	if service.Spec.Backend.Protocol != "http" && service.Spec.Backend.Protocol != "https" {
		return runtimeSite{}, nil, nil, ReasonValidationFailed, fmt.Errorf("backend.protocol %q is not supported for https publishing", service.Spec.Backend.Protocol)
	}
	if service.Spec.TLS == nil || service.Spec.TLS.Mode != publishv1alpha1.TLSModeSharedSAN {
		return runtimeSite{}, nil, nil, ReasonValidationFailed, fmt.Errorf("tls.mode sharedSAN is required for https publishing")
	}

	readyMatches := make([]certificatev1alpha1.CertificateBundle, 0, 1)
	selectorMatches := make([]certificatev1alpha1.CertificateBundle, 0, 1)
	for _, bundle := range bundles {
		if bundle.Namespace != service.Namespace || !bundleSelectsService(bundle, service) {
			continue
		}
		selectorMatches = append(selectorMatches, bundle)
		if bundle.Status.State != "Ready" || bundle.Status.CertificateSecretRef == nil {
			continue
		}
		if !domainCovered(bundle.Status.EffectiveDomains, service.Spec.Hostname) {
			continue
		}
		readyMatches = append(readyMatches, bundle)
	}

	if len(readyMatches) == 0 {
		switch {
		case len(selectorMatches) == 0:
			return runtimeSite{}, nil, nil, ReasonReferencesUnresolved, fmt.Errorf("no certificate bundle selects the published service")
		case selectorMatchNeedsCoverage(selectorMatches, service.Spec.Hostname):
			return runtimeSite{}, nil, nil, ReasonReferencesUnresolved, fmt.Errorf("selected certificate bundle does not yet cover hostname %q", service.Spec.Hostname)
		default:
			return runtimeSite{}, nil, nil, ReasonReferencesUnresolved, fmt.Errorf("selected certificate bundle is not ready")
		}
	}
	if len(readyMatches) > 1 {
		return runtimeSite{}, nil, nil, ReasonReferencesUnresolved, fmt.Errorf("multiple ready certificate bundles cover hostname %q", service.Spec.Hostname)
	}

	bundle := readyMatches[0]
	secretRef := bundle.Status.CertificateSecretRef.DeepCopy()
	if secretRef.Namespace == "" {
		secretRef.Namespace = bundle.Namespace
	}
	secretKey := types.NamespacedName{Name: secretRef.Name, Namespace: secretRef.Namespace}
	secret, found := bundleSecrets[secretKey]
	if !found {
		return runtimeSite{}, &common.ObjectReference{Name: bundle.Name, Namespace: bundle.Namespace}, secretRef, ReasonReferencesUnresolved, fmt.Errorf("certificate secret %s/%s was not found", secretRef.Namespace, secretRef.Name)
	}
	if len(secret.Data[corev1.TLSCertKey]) == 0 || len(secret.Data[corev1.TLSPrivateKeyKey]) == 0 {
		return runtimeSite{}, &common.ObjectReference{Name: bundle.Name, Namespace: bundle.Namespace}, secretRef, ReasonReferencesUnresolved, fmt.Errorf("certificate secret %s/%s is missing tls data", secretRef.Namespace, secretRef.Name)
	}

	certFile, keyFile := certificateFileNames(secret.Name)
	return runtimeSite{
		Hostname:            service.Spec.Hostname,
		BackendAddress:      service.Spec.Backend.Address,
		BackendPort:         service.Spec.Backend.Port,
		BackendProtocol:     service.Spec.Backend.Protocol,
		InsecureSkipVerify:  service.Spec.Backend.Transport != nil && service.Spec.Backend.Transport.InsecureSkipVerify,
		CertificateCertFile: runtimeCertificateMountPath + "/" + certFile,
		CertificateKeyFile:  runtimeCertificateMountPath + "/" + keyFile,
	}, &common.ObjectReference{Name: bundle.Name, Namespace: bundle.Namespace}, secretRef, "", nil
}

func bundleSelectsService(bundle certificatev1alpha1.CertificateBundle, service publishv1alpha1.PublishedService) bool {
	selector := labels.Everything()
	if bundle.Spec.PublishedServiceSelector != nil && len(bundle.Spec.PublishedServiceSelector.MatchLabels) > 0 {
		selector = labels.SelectorFromSet(bundle.Spec.PublishedServiceSelector.MatchLabels)
	}
	return selector.Matches(labels.Set(service.Labels))
}

func selectorMatchNeedsCoverage(bundles []certificatev1alpha1.CertificateBundle, hostname string) bool {
	for _, bundle := range bundles {
		if domainCovered(bundle.Status.EffectiveDomains, hostname) {
			return false
		}
	}
	return true
}

func domainCovered(domains []string, hostname string) bool {
	for _, domain := range domains {
		if domain == hostname {
			return true
		}
	}
	return false
}

func renderCaddyfile(sites []runtimeSite) string {
	var builder strings.Builder

	if len(sites) == 0 {
		builder.WriteString("# No published HTTPS services configured.\n")
	} else {
		builder.WriteString(":80 {\n")
		builder.WriteString("\tredir https://{host}{uri} permanent\n")
		builder.WriteString("}\n\n")
	}

	for _, site := range sites {
		builder.WriteString(site.Hostname)
		builder.WriteString(" {\n")
		builder.WriteString("\troute /* {\n")
		builder.WriteString(fmt.Sprintf("\t\treverse_proxy %s://%s:%d {\n", site.BackendProtocol, site.BackendAddress, site.BackendPort))
		builder.WriteString("\t\t\theader_up Host {host}\n")
		builder.WriteString("\t\t\theader_up X-Real-IP {remote_host}\n")
		builder.WriteString("\t\t\theader_up X-Forwarded-For {remote_host}\n")
		builder.WriteString("\t\t\theader_up X-Forwarded-Proto https\n")
		if site.BackendProtocol == "https" && site.InsecureSkipVerify {
			builder.WriteString("\t\t\ttransport http {\n")
			builder.WriteString("\t\t\t\ttls_insecure_skip_verify\n")
			builder.WriteString("\t\t\t}\n")
		}
		builder.WriteString("\t\t}\n")
		builder.WriteString("\t}\n\n")
		builder.WriteString("\ttls ")
		builder.WriteString(site.CertificateCertFile)
		builder.WriteString(" ")
		builder.WriteString(site.CertificateKeyFile)
		builder.WriteString("\n")
		builder.WriteString("\tlog {\n")
		builder.WriteString("\t\toutput stdout\n")
		builder.WriteString("\t\tformat console\n")
		builder.WriteString("\t}\n")
		builder.WriteString("}\n\n")
	}

	return builder.String()
}

func certificateFileNames(secretName string) (string, string) {
	return secretName + ".crt", secretName + ".key"
}

func sha1Hex(input string) string {
	sum := sha1.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}
