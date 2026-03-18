package certificate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"sort"
	"time"

	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	BundleStateReady   = "Ready"
	BundleStatePending = "Pending"
	DefaultCertTTL     = 30 * 24 * time.Hour
)

func EffectiveDomains(
	bundle certificatev1alpha1.CertificateBundle,
	services []publishv1alpha1.PublishedService,
	serviceHostnameValidator func(string) error,
) ([]string, error) {
	selector := labels.Everything()
	if bundle.Spec.PublishedServiceSelector != nil && len(bundle.Spec.PublishedServiceSelector.MatchLabels) > 0 {
		selector = labels.SelectorFromSet(bundle.Spec.PublishedServiceSelector.MatchLabels)
	}

	seen := map[string]struct{}{}
	for _, domain := range bundle.Spec.AdditionalDomains {
		if domain != "" {
			seen[domain] = struct{}{}
		}
	}

	for _, service := range services {
		if !selector.Matches(labels.Set(service.Labels)) {
			continue
		}
		if service.Spec.PublishMode != publishv1alpha1.PublishModeHTTPSProxy {
			continue
		}
		if service.Spec.TLS == nil || service.Spec.TLS.Mode != publishv1alpha1.TLSModeSharedSAN {
			continue
		}
		if serviceHostnameValidator != nil {
			if err := serviceHostnameValidator(service.Spec.Hostname); err != nil {
				return nil, fmt.Errorf("selected published service %s/%s has invalid hostname: %w", service.Namespace, service.Name, err)
			}
		}
		seen[service.Spec.Hostname] = struct{}{}
	}

	domains := make([]string, 0, len(seen))
	for domain := range seen {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains, nil
}

func BuildTLSSecret(name, namespace string, domains []string, validity time.Duration) (*corev1.Secret, *time.Time, error) {
	if len(domains) == 0 {
		return nil, nil, fmt.Errorf("at least one domain is required")
	}
	if validity <= 0 {
		validity = DefaultCertTTL
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial number: %w", err)
	}

	notBefore := time.Now().UTC().Add(-1 * time.Hour)
	notAfter := notBefore.Add(validity)

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: domains[0],
		},
		DNSNames:              domains,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	secret := &corev1.Secret{
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
	}
	secret.Name = name
	secret.Namespace = namespace

	return secret, &notAfter, nil
}
