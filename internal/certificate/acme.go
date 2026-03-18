package certificate

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	legocert "github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/registration"
	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	acmeAccountSecretSuffix     = "-acme-account"
	acmeRegistrationKey         = "registration.json"
	acmePrivateKeyKey           = "private.key"
	defaultRenewBefore          = 30 * 24 * time.Hour
	defaultChallengeTimeout     = 10 * time.Second
	letsEncryptDirectory        = "https://acme-v02.api.letsencrypt.org/directory"
	letsEncryptStagingDirectory = "https://acme-staging-v02.api.letsencrypt.org/directory"
)

var defaultRecursiveResolvers = []string{"8.8.8.8:53", "1.1.1.1:53"}

type Issuer interface {
	EnsureCertificate(context.Context, EnsureRequest) (EnsureResult, error)
}

type EnsureRequest struct {
	Bundle                certificatev1alpha1.CertificateBundle
	Domains               []string
	CloudflareAPIToken    string
	ExistingTLSSecret     *corev1.Secret
	ExistingAccountSecret *corev1.Secret
}

type EnsureResult struct {
	TLSSecret     *corev1.Secret
	AccountSecret *corev1.Secret
	ExpiresAt     *time.Time
	Issued        bool
}

type ACMEIssuer struct {
	now       func() time.Time
	preflight func(context.Context, string, []string, []string) error
	newClient func(*acmeUser, string, string) (*lego.Client, error)
	resolvers []string
}

type acmeUser struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

func NewACMEIssuer() *ACMEIssuer {
	return &ACMEIssuer{
		now:       func() time.Time { return time.Now().UTC() },
		preflight: preflightDNSChallenges,
		newClient: newLegoClient,
		resolvers: append([]string(nil), defaultRecursiveResolvers...),
	}
}

func (u *acmeUser) GetEmail() string                        { return u.Email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.Registration }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func AccountSecretName(bundleName string) string {
	return bundleName + acmeAccountSecretSuffix
}

func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errString := strings.ToLower(err.Error())
	return strings.Contains(errString, "rate limit") ||
		strings.Contains(errString, "too many certificates") ||
		strings.Contains(errString, "ratelimited")
}

func (i *ACMEIssuer) EnsureCertificate(ctx context.Context, request EnsureRequest) (EnsureResult, error) {
	if i.preflight == nil {
		i.preflight = preflightDNSChallenges
	}
	if i.newClient == nil {
		i.newClient = newLegoClient
	}
	if len(i.resolvers) == 0 {
		i.resolvers = append([]string(nil), defaultRecursiveResolvers...)
	}
	if i.now == nil {
		i.now = func() time.Time { return time.Now().UTC() }
	}
	request.Domains = normalizeDomains(request.Domains)
	if len(request.Domains) == 0 {
		return EnsureResult{}, fmt.Errorf("at least one domain is required")
	}
	if request.Bundle.Spec.SecretTemplate.Name == "" {
		return EnsureResult{}, fmt.Errorf("certificate secret template name is required")
	}
	if request.CloudflareAPIToken == "" {
		return EnsureResult{}, fmt.Errorf("cloudflare api token is required")
	}

	renewBefore := request.Bundle.Spec.RenewBefore.Duration
	if renewBefore <= 0 {
		renewBefore = defaultRenewBefore
	}
	now := i.now()

	if reusable, expiresAt, err := reusableCertificate(request.ExistingTLSSecret, request.Domains, renewBefore, now); err == nil && reusable {
		return EnsureResult{
			TLSSecret: request.ExistingTLSSecret.DeepCopy(),
			ExpiresAt: &expiresAt,
			Issued:    false,
		}, nil
	}

	if err := i.preflight(ctx, request.CloudflareAPIToken, request.Domains, i.resolvers); err != nil {
		return EnsureResult{}, WrapIssueError(FailureClassDNSPreflight, fmt.Errorf("dns preflight failed: %w", err))
	}

	user, accountSecret, err := loadOrCreateACMEUser(
		request.Bundle.Namespace,
		AccountSecretName(request.Bundle.Name),
		request.Bundle.Spec.Issuer.Email,
		request.ExistingAccountSecret,
	)
	if err != nil {
		return EnsureResult{}, WrapIssueError(FailureClassIssue, err)
	}

	client, err := i.newClient(user, request.Bundle.Spec.Issuer.Provider, request.CloudflareAPIToken)
	if err != nil {
		return EnsureResult{}, WrapIssueError(FailureClassIssue, err)
	}

	if user.Registration == nil {
		registrationResource, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return EnsureResult{}, classifyIssuanceError(fmt.Errorf("register acme account: %w", err))
		}
		user.Registration = registrationResource
		accountSecret, err = buildAccountSecret(request.Bundle.Namespace, AccountSecretName(request.Bundle.Name), user)
		if err != nil {
			return EnsureResult{}, WrapIssueError(FailureClassIssue, err)
		}
	}

	obtainRequest := legocert.ObtainRequest{
		Domains: append([]string(nil), request.Domains...),
		Bundle:  true,
	}
	resource, err := client.Certificate.Obtain(obtainRequest)
	if err != nil {
		return EnsureResult{}, classifyIssuanceError(fmt.Errorf("obtain certificate: %w", err))
	}

	expiresAt, err := certificateExpiry(resource.Certificate)
	if err != nil {
		return EnsureResult{}, WrapIssueError(FailureClassIssue, err)
	}

	secret := &corev1.Secret{
		Type: corev1.SecretTypeTLS,
		ObjectMeta: metav1.ObjectMeta{
			Name:      request.Bundle.Spec.SecretTemplate.Name,
			Namespace: request.Bundle.Namespace,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       append([]byte(nil), resource.Certificate...),
			corev1.TLSPrivateKeyKey: append([]byte(nil), resource.PrivateKey...),
		},
	}

	return EnsureResult{
		TLSSecret:     secret,
		AccountSecret: accountSecret,
		ExpiresAt:     &expiresAt,
		Issued:        true,
	}, nil
}

func classifyIssuanceError(err error) error {
	if IsRateLimitError(err) {
		return WrapIssueError(FailureClassRateLimited, err)
	}
	return WrapIssueError(FailureClassIssue, err)
}

func newLegoClient(user *acmeUser, providerName, cloudflareToken string) (*lego.Client, error) {
	if user == nil {
		return nil, fmt.Errorf("acme user is required")
	}

	config := lego.NewConfig(user)
	config.Certificate.KeyType = certcrypto.RSA2048

	switch providerName {
	case certificatev1alpha1.CertificateIssuerLetsEncrypt:
		config.CADirURL = letsEncryptDirectory
	case certificatev1alpha1.CertificateIssuerLetsEncryptStaged:
		config.CADirURL = letsEncryptStagingDirectory
	default:
		return nil, fmt.Errorf("unsupported issuer provider %q", providerName)
	}

	dnsConfig := cloudflare.NewDefaultConfig()
	dnsConfig.AuthToken = cloudflareToken
	dnsProvider, err := cloudflare.NewDNSProviderConfig(dnsConfig)
	if err != nil {
		return nil, fmt.Errorf("create cloudflare dns provider: %w", err)
	}

	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("create lego client: %w", err)
	}

	options := []dns01.ChallengeOption{
		dns01.AddRecursiveNameservers(defaultRecursiveResolvers),
		dns01.AddDNSTimeout(defaultChallengeTimeout),
		dns01.DisableCompletePropagationRequirement(),
	}
	if err := client.Challenge.SetDNS01Provider(dnsProvider, options...); err != nil {
		return nil, fmt.Errorf("configure dns01 provider: %w", err)
	}

	return client, nil
}

func loadOrCreateACMEUser(namespace, name, email string, existing *corev1.Secret) (*acmeUser, *corev1.Secret, error) {
	if existing != nil {
		user, err := acmeUserFromSecret(existing, email)
		if err != nil {
			return nil, nil, err
		}
		return user, existing.DeepCopy(), nil
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate acme account key: %w", err)
	}
	user := &acmeUser{
		Email: email,
		key:   privateKey,
	}
	accountSecret, err := buildAccountSecret(namespace, name, user)
	if err != nil {
		return nil, nil, err
	}
	return user, accountSecret, nil
}

func acmeUserFromSecret(secret *corev1.Secret, email string) (*acmeUser, error) {
	keyData, found := secret.Data[acmePrivateKeyKey]
	if !found || len(keyData) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing acme private key", secret.Namespace, secret.Name)
	}
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("decode acme private key from secret %s/%s", secret.Namespace, secret.Name)
	}
	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse acme private key from secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}

	user := &acmeUser{
		Email: email,
		key:   privateKey,
	}
	if registrationData, found := secret.Data[acmeRegistrationKey]; found && len(registrationData) > 0 {
		var resource registration.Resource
		if err := json.Unmarshal(registrationData, &resource); err != nil {
			return nil, fmt.Errorf("parse acme registration from secret %s/%s: %w", secret.Namespace, secret.Name, err)
		}
		user.Registration = &resource
	}
	return user, nil
}

func buildAccountSecret(namespace, name string, user *acmeUser) (*corev1.Secret, error) {
	privateKey, ok := user.key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("acme account private key must be ECDSA")
	}
	privateKeyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshal acme account private key: %w", err)
	}
	registrationBytes := []byte{}
	if user.Registration != nil {
		registrationBytes, err = json.Marshal(user.Registration)
		if err != nil {
			return nil, fmt.Errorf("marshal acme registration: %w", err)
		}
	}

	return &corev1.Secret{
		Type: corev1.SecretTypeOpaque,
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			acmeRegistrationKey: registrationBytes,
			acmePrivateKeyKey: pem.EncodeToMemory(&pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: privateKeyBytes,
			}),
		},
	}, nil
}

func reusableCertificate(secret *corev1.Secret, domains []string, renewBefore time.Duration, now time.Time) (bool, time.Time, error) {
	if secret == nil {
		return false, time.Time{}, fmt.Errorf("existing tls secret is missing")
	}
	certificatePEM, found := secret.Data[corev1.TLSCertKey]
	if !found || len(certificatePEM) == 0 {
		return false, time.Time{}, fmt.Errorf("existing tls secret is missing certificate data")
	}

	certificate, err := parseLeafCertificate(certificatePEM)
	if err != nil {
		return false, time.Time{}, err
	}
	if now.Add(renewBefore).After(certificate.NotAfter) {
		return false, certificate.NotAfter, nil
	}
	for _, domain := range domains {
		if err := certificate.VerifyHostname(domain); err != nil {
			return false, certificate.NotAfter, nil
		}
	}
	return true, certificate.NotAfter, nil
}

func certificateExpiry(certificatePEM []byte) (time.Time, error) {
	certificate, err := parseLeafCertificate(certificatePEM)
	if err != nil {
		return time.Time{}, err
	}
	return certificate.NotAfter.UTC(), nil
}

func parseLeafCertificate(certificatePEM []byte) (*x509.Certificate, error) {
	for len(certificatePEM) > 0 {
		var block *pem.Block
		block, certificatePEM = pem.Decode(certificatePEM)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		return certificate, nil
	}
	return nil, fmt.Errorf("no certificate found in pem data")
}

func normalizeDomains(domains []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		normalized := strings.ToLower(strings.TrimSpace(domain))
		if normalized == "" {
			continue
		}
		if _, found := seen[normalized]; found {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}
