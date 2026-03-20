package migration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	"github.com/jerkytreats/dns-operator/api/common"
	dnsv1alpha1 "github.com/jerkytreats/dns-operator/api/dns/v1alpha1"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	tailscalev1alpha1 "github.com/jerkytreats/dns-operator/api/tailscale/v1alpha1"
	yamlv3 "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	DefaultNamespace                 = "dns-operator-system"
	DefaultBundleName                = "internal-shared"
	DefaultCloudflareSecretName      = "cloudflare-credentials"
	DefaultTailscaleSecretName       = "tailscale-admin-credentials"
	DefaultCertificateSecretTemplate = "internal-example-test-shared-tls"
)

type ImportInput struct {
	Namespace              string
	BundleName             string
	NameserverAddress      string
	ConfigYAML             []byte
	ZoneFile               []byte
	ProxyRulesJSON         []byte
	CertificateDomainsJSON []byte
	Caddyfile              []byte
}

type ImportReport struct {
	Namespace                 string              `json:"namespace"`
	ManagedZone               string              `json:"managedZone,omitempty"`
	NameserverAddress         string              `json:"nameserverAddress,omitempty"`
	ImportedObjectCounts      map[string]int      `json:"importedObjectCounts"`
	SkippedDisabledProxyRules []string            `json:"skippedDisabledProxyRules,omitempty"`
	CaseCollisions            map[string][]string `json:"caseCollisions,omitempty"`
	Warnings                  []string            `json:"warnings,omitempty"`
}

type ImportResult struct {
	Objects []any
	Report  ImportReport
}

type legacyConfig struct {
	Tailscale struct {
		APIKey  string `yaml:"api_key"`
		Tailnet string `yaml:"tailnet"`
		DNS     struct {
			Zone string `yaml:"zone"`
		} `yaml:"dns"`
	} `yaml:"tailscale"`
	Certificate struct {
		Email              string `yaml:"email"`
		Domain             string `yaml:"domain"`
		CloudflareAPIToken string `yaml:"cloudflare_api_token"`
		UseProductionCerts bool   `yaml:"use_production_certs"`
		Renewal            struct {
			RenewBefore string `yaml:"renew_before"`
		} `yaml:"renewal"`
	} `yaml:"certificate"`
}

type legacyProxyRule struct {
	Hostname   string    `json:"hostname"`
	TargetIP   string    `json:"target_ip"`
	TargetPort int       `json:"target_port"`
	Protocol   string    `json:"protocol"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

type legacyCertificateDomains struct {
	BaseDomain string    `json:"base_domain"`
	SANDomains []string  `json:"san_domains"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type importedRecord struct {
	Hostname string
	Type     string
	TTL      int32
	Values   []string
}

type caddyTransportHints map[string]publishv1alpha1.PublishBackendTransport

func Import(input ImportInput) (ImportResult, error) {
	namespace := input.Namespace
	if namespace == "" {
		namespace = DefaultNamespace
	}
	bundleName := input.BundleName
	if bundleName == "" {
		bundleName = DefaultBundleName
	}

	report := ImportReport{
		Namespace:            namespace,
		NameserverAddress:    input.NameserverAddress,
		ImportedObjectCounts: map[string]int{},
		CaseCollisions:       map[string][]string{},
	}

	cfg, err := parseConfig(input.ConfigYAML)
	if err != nil {
		return ImportResult{}, err
	}

	managedZone := strings.ToLower(strings.TrimSuffix(cfg.Tailscale.DNS.Zone, "."))
	if managedZone == "" {
		managedZone = strings.ToLower(strings.TrimSuffix(cfg.Certificate.Domain, "."))
	}

	records, zoneOrigin, zoneWarnings, zoneCollisions, err := parseZoneFile(input.ZoneFile)
	if err != nil {
		return ImportResult{}, err
	}
	report.Warnings = append(report.Warnings, zoneWarnings...)
	mergeCaseCollisions(report.CaseCollisions, zoneCollisions)
	if managedZone == "" {
		managedZone = zoneOrigin
	}
	report.ManagedZone = managedZone

	transportHints, err := parseCaddyfileTransportHints(input.Caddyfile)
	if err != nil {
		return ImportResult{}, err
	}

	proxyRules, disabledRules, proxyCollisions, err := parseProxyRules(input.ProxyRulesJSON)
	if err != nil {
		return ImportResult{}, err
	}
	report.SkippedDisabledProxyRules = disabledRules
	mergeCaseCollisions(report.CaseCollisions, proxyCollisions)

	certDomains, certCollisions, err := parseCertificateDomains(input.CertificateDomainsJSON)
	if err != nil {
		return ImportResult{}, err
	}
	mergeCaseCollisions(report.CaseCollisions, certCollisions)

	objects := make([]any, 0)
	if cfg.Tailscale.APIKey != "" {
		objects = append(objects, buildSecret(namespace, DefaultTailscaleSecretName, "api-key", cfg.Tailscale.APIKey))
		report.ImportedObjectCounts["Secret"]++
	}
	if cfg.Certificate.CloudflareAPIToken != "" {
		objects = append(objects, buildSecret(namespace, DefaultCloudflareSecretName, "api-token", cfg.Certificate.CloudflareAPIToken))
		report.ImportedObjectCounts["Secret"]++
	}

	dnsRecords := buildDNSRecords(namespace, records)
	for _, record := range dnsRecords {
		objects = append(objects, record)
	}
	report.ImportedObjectCounts["DNSRecord"] += len(dnsRecords)

	publishedServices := buildPublishedServices(namespace, bundleName, proxyRules, transportHints)
	for _, service := range publishedServices {
		objects = append(objects, service)
	}
	report.ImportedObjectCounts["PublishedService"] += len(publishedServices)

	if bundle, ok := buildCertificateBundle(namespace, bundleName, cfg, certDomains, len(publishedServices) > 0); ok {
		objects = append(objects, bundle)
		report.ImportedObjectCounts["CertificateBundle"]++
	}

	if tailnetConfig, ok := buildTailnetDNSConfig(namespace, managedZone, input.NameserverAddress, cfg); ok {
		objects = append(objects, tailnetConfig)
		report.ImportedObjectCounts["TailnetDNSConfig"]++
	} else if cfg.Tailscale.Tailnet != "" {
		report.Warnings = append(report.Warnings, "tailscale tailnet config was present but no nameserver address was supplied, so TailnetDNSConfig was not emitted")
	}

	return ImportResult{Objects: objects, Report: report}, nil
}

func RenderYAML(objects []any) ([]byte, error) {
	var out bytes.Buffer
	for i, object := range objects {
		data, err := yaml.Marshal(object)
		if err != nil {
			return nil, fmt.Errorf("marshal object %d: %w", i, err)
		}
		if i > 0 {
			out.WriteString("---\n")
		}
		out.Write(data)
	}
	return out.Bytes(), nil
}

func RenderReport(report ImportReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func parseConfig(data []byte) (legacyConfig, error) {
	var cfg legacyConfig
	if len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil
	}
	if err := yamlv3.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config.yaml: %w", err)
	}
	return cfg, nil
}

func parseProxyRules(data []byte) ([]legacyProxyRule, []string, map[string][]string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil, nil, nil
	}

	var raw map[string]legacyProxyRule
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, nil, fmt.Errorf("parse proxy_rules.json: %w", err)
	}

	collisions := map[string][]string{}
	disabled := make([]string, 0)
	seen := map[string]legacyProxyRule{}
	for key, rule := range raw {
		hostname := rule.Hostname
		if hostname == "" {
			hostname = key
		}
		canonical := canonicalHostname(hostname)
		if canonical == "" {
			continue
		}
		recordCollision(collisions, canonical, hostname)
		rule.Hostname = canonical
		if !rule.Enabled {
			disabled = append(disabled, canonical)
			continue
		}
		seen[canonical] = rule
	}

	rules := make([]legacyProxyRule, 0, len(seen))
	for _, rule := range seen {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Hostname < rules[j].Hostname })
	sort.Strings(disabled)
	return rules, disabled, collisions, nil
}

func parseCertificateDomains(data []byte) (legacyCertificateDomains, map[string][]string, error) {
	var domains legacyCertificateDomains
	if len(bytes.TrimSpace(data)) == 0 {
		return domains, nil, nil
	}
	if err := json.Unmarshal(data, &domains); err != nil {
		return domains, nil, fmt.Errorf("parse certificate_domains.json: %w", err)
	}

	collisions := map[string][]string{}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(domains.SANDomains))
	for _, domain := range domains.SANDomains {
		canonical := canonicalHostname(domain)
		if canonical == "" {
			continue
		}
		recordCollision(collisions, canonical, domain)
		if _, found := seen[canonical]; found {
			continue
		}
		seen[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}
	sort.Strings(normalized)
	domains.BaseDomain = canonicalHostname(domains.BaseDomain)
	domains.SANDomains = normalized
	return domains, collisions, nil
}

func parseZoneFile(data []byte) ([]importedRecord, string, []string, map[string][]string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, "", nil, nil, nil
	}

	var origin string
	collisions := map[string][]string{}
	aggregated := map[string]importedRecord{}
	warnings := []string{}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	inSOA := false
	for scanner.Scan() {
		line := stripComment(scanner.Text())
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if inSOA {
			if strings.Contains(line, ")") {
				inSOA = false
			}
			continue
		}
		if strings.HasPrefix(line, "$ORIGIN") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				origin = canonicalHostname(fields[1])
			}
			continue
		}
		if strings.HasPrefix(line, "$TTL") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		inIndex := indexOf(fields, "IN")
		if inIndex <= 0 || inIndex+2 > len(fields) {
			continue
		}

		name := fields[0]
		recordType := strings.ToUpper(fields[inIndex+1])
		if recordType == "SOA" {
			if origin == "" {
				origin = expandZoneName(name, "")
			}
			inSOA = strings.Contains(line, "(") && !strings.Contains(line, ")")
			continue
		}
		if recordType == "NS" {
			if origin == "" && strings.HasSuffix(name, ".") {
				origin = canonicalHostname(name)
			}
			continue
		}
		if recordType != dnsv1alpha1.DNSRecordTypeA &&
			recordType != dnsv1alpha1.DNSRecordTypeAAAA &&
			recordType != dnsv1alpha1.DNSRecordTypeCNAME &&
			recordType != dnsv1alpha1.DNSRecordTypeTXT {
			warnings = append(warnings, fmt.Sprintf("skipping unsupported zone record type %q", recordType))
			continue
		}

		ttl := int32(300)
		if inIndex >= 2 {
			if parsedTTL, err := strconv.Atoi(fields[inIndex-1]); err == nil {
				ttl = int32(parsedTTL)
			}
		}

		hostname := expandZoneName(name, origin)
		if hostname == "" {
			continue
		}
		recordCollision(collisions, hostname, expandZoneNamePreserveCase(name, origin))

		value := strings.Join(fields[inIndex+2:], " ")
		value = strings.TrimSpace(value)
		if recordType == dnsv1alpha1.DNSRecordTypeTXT {
			value = strings.Trim(value, "\"")
		}
		key := fmt.Sprintf("%s|%s|%d", hostname, recordType, ttl)
		record := aggregated[key]
		if record.Hostname == "" {
			record = importedRecord{Hostname: hostname, Type: recordType, TTL: ttl}
		}
		record.Values = append(record.Values, value)
		aggregated[key] = record
	}
	if err := scanner.Err(); err != nil {
		return nil, "", nil, nil, fmt.Errorf("scan zone file: %w", err)
	}

	records := make([]importedRecord, 0, len(aggregated))
	for _, record := range aggregated {
		record.Values = uniqueStrings(record.Values)
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Hostname == records[j].Hostname {
			return records[i].Type < records[j].Type
		}
		return records[i].Hostname < records[j].Hostname
	})
	return records, origin, warnings, collisions, nil
}

func parseCaddyfileTransportHints(data []byte) (caddyTransportHints, error) {
	hints := caddyTransportHints{}
	if len(bytes.TrimSpace(data)) == 0 {
		return hints, nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	currentHost := ""
	depth := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case currentHost == "" && strings.HasSuffix(line, "{") && isTopLevelHostLabel(strings.TrimSuffix(line, "{")):
			currentHost = canonicalHostname(strings.TrimSpace(strings.TrimSuffix(line, "{")))
			depth = 1
		case currentHost != "":
			depth += strings.Count(line, "{")
			depth -= strings.Count(line, "}")
			if strings.Contains(line, "tls_insecure_skip_verify") {
				hints[currentHost] = publishv1alpha1.PublishBackendTransport{InsecureSkipVerify: true}
			}
			if depth <= 0 {
				currentHost = ""
				depth = 0
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan Caddyfile: %w", err)
	}
	return hints, nil
}

func buildSecret(namespace, name, key, value string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    baseLabels(),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{key: value},
	}
}

func buildDNSRecords(namespace string, imported []importedRecord) []*dnsv1alpha1.DNSRecord {
	records := make([]*dnsv1alpha1.DNSRecord, 0, len(imported))
	for _, record := range imported {
		name := resourceName(record.Hostname)
		records = append(records, &dnsv1alpha1.DNSRecord{
			TypeMeta: metav1.TypeMeta{APIVersion: "dns.jerkytreats.dev/v1alpha1", Kind: "DNSRecord"},
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Labels:      baseLabels(),
				Annotations: map[string]string{"migration.jerkytreats.dev/source": "zone-file"},
			},
			Spec: dnsv1alpha1.DNSRecordSpec{
				Hostname: record.Hostname,
				Type:     record.Type,
				TTL:      record.TTL,
				Values:   append([]string(nil), record.Values...),
			},
		})
	}
	return records
}

func buildPublishedServices(namespace, bundleName string, rules []legacyProxyRule, hints caddyTransportHints) []*publishv1alpha1.PublishedService {
	services := make([]*publishv1alpha1.PublishedService, 0, len(rules))
	for _, rule := range rules {
		labels := baseLabels()
		labels["publish.jerkytreats.dev/certificate-bundle"] = bundleName
		annotations := map[string]string{
			"migration.jerkytreats.dev/source": "proxy_rules.json",
		}
		if !rule.CreatedAt.IsZero() {
			annotations["migration.jerkytreats.dev/created-at"] = rule.CreatedAt.UTC().Format(time.RFC3339)
		}

		service := &publishv1alpha1.PublishedService{
			TypeMeta: metav1.TypeMeta{APIVersion: "publish.jerkytreats.dev/v1alpha1", Kind: "PublishedService"},
			ObjectMeta: metav1.ObjectMeta{
				Name:        resourceName(rule.Hostname),
				Namespace:   namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: publishv1alpha1.PublishedServiceSpec{
				Hostname:    rule.Hostname,
				PublishMode: publishv1alpha1.PublishModeHTTPSProxy,
				Backend: &publishv1alpha1.PublishBackend{
					Address:  rule.TargetIP,
					Port:     int32(rule.TargetPort),
					Protocol: strings.ToLower(rule.Protocol),
				},
				TLS:  &publishv1alpha1.PublishTLS{Mode: publishv1alpha1.TLSModeSharedSAN},
				Auth: &publishv1alpha1.PublishAuth{Mode: publishv1alpha1.AuthModeNone},
			},
		}
		if hint, found := hints[rule.Hostname]; found && hint.InsecureSkipVerify {
			service.Spec.Backend.Transport = &publishv1alpha1.PublishBackendTransport{InsecureSkipVerify: true}
		}
		services = append(services, service)
	}
	return services
}

func buildCertificateBundle(namespace, bundleName string, cfg legacyConfig, domains legacyCertificateDomains, hasPublishedServices bool) (*certificatev1alpha1.CertificateBundle, bool) {
	if cfg.Certificate.Email == "" && cfg.Certificate.CloudflareAPIToken == "" && domains.BaseDomain == "" && len(domains.SANDomains) == 0 && !hasPublishedServices {
		return nil, false
	}

	provider := certificatev1alpha1.CertificateIssuerLetsEncryptStaged
	if cfg.Certificate.UseProductionCerts {
		provider = certificatev1alpha1.CertificateIssuerLetsEncrypt
	}

	additionalDomains := make([]string, 0, len(domains.SANDomains)+1)
	if domains.BaseDomain != "" {
		additionalDomains = append(additionalDomains, domains.BaseDomain)
	}
	additionalDomains = append(additionalDomains, domains.SANDomains...)
	additionalDomains = uniqueStrings(additionalDomains)

	bundle := &certificatev1alpha1.CertificateBundle{
		TypeMeta: metav1.TypeMeta{APIVersion: "certificate.jerkytreats.dev/v1alpha1", Kind: "CertificateBundle"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        bundleName,
			Namespace:   namespace,
			Labels:      baseLabels(),
			Annotations: map[string]string{"migration.jerkytreats.dev/source": "certificate_domains.json"},
		},
		Spec: certificatev1alpha1.CertificateBundleSpec{
			Mode: certificatev1alpha1.CertificateBundleModeSharedSAN,
			PublishedServiceSelector: &common.ServiceSelector{
				MatchLabels: map[string]string{"publish.jerkytreats.dev/certificate-bundle": bundleName},
			},
			AdditionalDomains: additionalDomains,
			Issuer: certificatev1alpha1.BundleIssuer{
				Provider: provider,
				Email:    cfg.Certificate.Email,
			},
			Challenge: certificatev1alpha1.BundleChallenge{
				Type: certificatev1alpha1.CertificateChallengeDNS01,
				Cloudflare: certificatev1alpha1.BundleCloudflare{
					APITokenSecretRef: common.SecretKeyReference{
						Name: DefaultCloudflareSecretName,
						Key:  "api-token",
					},
				},
			},
			SecretTemplate: certificatev1alpha1.BundleSecretTemplate{Name: DefaultCertificateSecretTemplate},
		},
	}
	if cfg.Certificate.Renewal.RenewBefore != "" {
		if duration, err := time.ParseDuration(cfg.Certificate.Renewal.RenewBefore); err == nil {
			bundle.Spec.RenewBefore = metav1.Duration{Duration: duration}
		}
	}
	return bundle, true
}

func buildTailnetDNSConfig(namespace, zone, nameserver string, cfg legacyConfig) (*tailscalev1alpha1.TailnetDNSConfig, bool) {
	if cfg.Tailscale.Tailnet == "" || zone == "" || nameserver == "" {
		return nil, false
	}
	return &tailscalev1alpha1.TailnetDNSConfig{
		TypeMeta: metav1.TypeMeta{APIVersion: "tailscale.jerkytreats.dev/v1alpha1", Kind: "TailnetDNSConfig"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "internal-zone",
			Namespace:   namespace,
			Labels:      baseLabels(),
			Annotations: map[string]string{"migration.jerkytreats.dev/source": "config.yaml+tailscale-admin-state"},
		},
		Spec: tailscalev1alpha1.TailnetDNSConfigSpec{
			Zone:    zone,
			Tailnet: cfg.Tailscale.Tailnet,
			Nameserver: tailscalev1alpha1.TailnetNameserver{
				Address: nameserver,
			},
			Auth: tailscalev1alpha1.TailnetDNSAuth{
				SecretRef: common.SecretKeyReference{
					Name: DefaultTailscaleSecretName,
					Key:  "api-key",
				},
			},
			Behavior: tailscalev1alpha1.TailnetBehavior{Mode: tailscalev1alpha1.TailnetDNSBehaviorBootstrapAndRepair},
		},
	}, true
}

func baseLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "dns-operator",
		"app.kubernetes.io/managed-by": "dns-operator-import",
	}
}

func mergeCaseCollisions(dst, src map[string][]string) {
	for key, values := range src {
		if len(values) == 0 {
			continue
		}
		existing := append(dst[key], values...)
		dst[key] = uniqueStrings(existing)
	}
}

func recordCollision(collisions map[string][]string, canonical, original string) {
	if canonical == "" || original == "" {
		return
	}
	normalizedOriginal := canonicalHostname(original)
	if normalizedOriginal != canonical {
		return
	}
	if strings.TrimSuffix(original, ".") == canonical {
		return
	}
	collisions[canonical] = uniqueStrings(append(collisions[canonical], original))
}

func canonicalHostname(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), "."))
}

func stripComment(line string) string {
	if idx := strings.Index(line, ";"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func expandZoneName(name, origin string) string {
	switch {
	case name == "@":
		return origin
	case strings.HasSuffix(name, "."):
		return canonicalHostname(name)
	case origin == "":
		return canonicalHostname(name)
	default:
		return canonicalHostname(name + "." + origin)
	}
}

func expandZoneNamePreserveCase(name, origin string) string {
	name = strings.TrimSpace(name)
	origin = strings.TrimSuffix(strings.TrimSpace(origin), ".")
	switch {
	case name == "@":
		return origin
	case strings.HasSuffix(name, "."):
		return strings.TrimSuffix(name, ".")
	case origin == "":
		return name
	default:
		return name + "." + origin
	}
}

func isTopLevelHostLabel(label string) bool {
	label = strings.TrimSpace(label)
	if label == "" || label == ":" || strings.HasPrefix(label, ":") {
		return false
	}
	if strings.Contains(label, " ") || strings.Contains(label, "\t") {
		return false
	}
	return strings.Contains(label, ".")
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func resourceName(hostname string) string {
	name := strings.ReplaceAll(hostname, ".", "-")
	if len(name) > 63 {
		return name[:63]
	}
	return name
}
