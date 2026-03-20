package dns

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"

	dnsv1alpha1 "github.com/jerkytreats/dns-operator/api/dns/v1alpha1"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	"github.com/jerkytreats/dns-operator/internal/validation"
)

const (
	ZoneSyncRequestName = "zone-sync"
	defaultTTL          = int32(300)
)

type AuthoritativeRecord struct {
	Hostname string
	Type     string
	TTL      int32
	Values   []string
}

type RenderedZone struct {
	Zone          string
	ConfigMapName string
	DataKey       string
	Content       string
	Hash          string
}

func ZoneConfigMapName(zone string) string {
	return "zone-" + strings.ReplaceAll(zone, ".", "-")
}

func ZoneConfigMapKey(zone string) string {
	return "db." + zone
}

func RecordForDNSRecord(policy validation.ZonePolicy, record dnsv1alpha1.DNSRecord) (AuthoritativeRecord, error) {
	if err := policy.ValidateAuthoritativeHostname(record.Spec.Hostname); err != nil {
		return AuthoritativeRecord{}, err
	}

	for _, value := range record.Spec.Values {
		if err := validateRecordValue(record.Spec.Type, value); err != nil {
			return AuthoritativeRecord{}, err
		}
	}

	ttl := record.Spec.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}

	return AuthoritativeRecord{
		Hostname: record.Spec.Hostname,
		Type:     record.Spec.Type,
		TTL:      ttl,
		Values:   append([]string(nil), record.Spec.Values...),
	}, nil
}

func RecordForPublishedService(policy validation.ZonePolicy, service publishv1alpha1.PublishedService) (AuthoritativeRecord, error) {
	if err := policy.ValidateAuthoritativeHostname(service.Spec.Hostname); err != nil {
		return AuthoritativeRecord{}, err
	}

	if service.Spec.Backend == nil || service.Spec.Backend.Address == "" {
		return AuthoritativeRecord{}, fmt.Errorf("backend.address is required")
	}

	recordType, value, err := validation.InferRecordFromAddress(service.Spec.Backend.Address)
	if err != nil {
		return AuthoritativeRecord{}, err
	}

	return AuthoritativeRecord{
		Hostname: service.Spec.Hostname,
		Type:     recordType,
		TTL:      defaultTTL,
		Values:   []string{value},
	}, nil
}

func RecordForPublishedServiceTarget(policy validation.ZonePolicy, service publishv1alpha1.PublishedService, target string) (AuthoritativeRecord, error) {
	if err := policy.ValidateAuthoritativeHostname(service.Spec.Hostname); err != nil {
		return AuthoritativeRecord{}, err
	}
	if target == "" {
		return AuthoritativeRecord{}, fmt.Errorf("publish runtime target is required")
	}

	recordType, value, err := validation.InferRecordFromAddress(target)
	if err != nil {
		return AuthoritativeRecord{}, err
	}

	return AuthoritativeRecord{
		Hostname: service.Spec.Hostname,
		Type:     recordType,
		TTL:      defaultTTL,
		Values:   []string{value},
	}, nil
}

func RenderZone(policy validation.ZonePolicy, records []AuthoritativeRecord) (RenderedZone, error) {
	aggregated := map[string]AuthoritativeRecord{}
	keys := make([]string, 0, len(records))
	zone := policy.AuthoritativeZone()

	for _, record := range records {
		if err := policy.ValidateAuthoritativeHostname(record.Hostname); err != nil {
			return RenderedZone{}, err
		}
		key := fmt.Sprintf("%s|%s|%d", record.Hostname, record.Type, record.TTL)
		existing, found := aggregated[key]
		if !found {
			existing = AuthoritativeRecord{
				Hostname: record.Hostname,
				Type:     record.Type,
				TTL:      record.TTL,
			}
			keys = append(keys, key)
		}
		existing.Values = append(existing.Values, record.Values...)
		aggregated[key] = existing
	}

	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	var builder strings.Builder
	builder.WriteString("$ORIGIN ")
	builder.WriteString(zone)
	builder.WriteString(".\n")
	builder.WriteString("$TTL 300\n")
	builder.WriteString("@ IN SOA ns1.")
	builder.WriteString(zone)
	builder.WriteString(". hostmaster.")
	builder.WriteString(zone)
	builder.WriteString(". (\n")
	builder.WriteString("  ")
	builder.WriteString(zoneSerial(keys, aggregated))
	builder.WriteString(" ; serial\n")
	builder.WriteString("  120 ; refresh\n")
	builder.WriteString("  60 ; retry\n")
	builder.WriteString("  604800 ; expire\n")
	builder.WriteString("  300 ; minimum\n")
	builder.WriteString(")\n")
	builder.WriteString("@ IN NS ns1.")
	builder.WriteString(zone)
	builder.WriteString(".\n")
	builder.WriteString("ns1 IN A 127.0.0.1\n")

	for _, key := range keys {
		record := aggregated[key]
		values := uniqueSorted(record.Values)
		for _, value := range values {
			relativeName, err := policy.RelativeName(record.Hostname)
			if err != nil {
				return RenderedZone{}, err
			}
			builder.WriteString(fmt.Sprintf("%s %d IN %s %s\n",
				relativeName,
				record.TTL,
				record.Type,
				renderValue(record.Type, value),
			))
		}
	}

	content := builder.String()

	return RenderedZone{
		Zone:          zone,
		ConfigMapName: ZoneConfigMapName(zone),
		DataKey:       ZoneConfigMapKey(zone),
		Content:       content,
		Hash:          sha1Hex(content),
	}, nil
}

func validateRecordValue(recordType, value string) error {
	switch recordType {
	case dnsv1alpha1.DNSRecordTypeA:
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("a records require an IPv4 value")
		}
	case dnsv1alpha1.DNSRecordTypeAAAA:
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() != nil {
			return fmt.Errorf("aaaa records require an IPv6 value")
		}
	case dnsv1alpha1.DNSRecordTypeCNAME:
		if err := validation.ValidateFQDN(value); err != nil {
			return fmt.Errorf("cname records require a valid target: %w", err)
		}
	case dnsv1alpha1.DNSRecordTypeTXT:
		if value == "" {
			return fmt.Errorf("txt records require a value")
		}
	default:
		return fmt.Errorf("unsupported record type %q", recordType)
	}

	return nil
}

func renderValue(recordType, value string) string {
	switch recordType {
	case dnsv1alpha1.DNSRecordTypeCNAME:
		if strings.HasSuffix(value, ".") {
			return value
		}
		return value + "."
	case dnsv1alpha1.DNSRecordTypeTXT:
		if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			return value
		}
		return fmt.Sprintf("%q", value)
	default:
		return value
	}
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
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

func zoneSerial(keys []string, aggregated map[string]AuthoritativeRecord) string {
	var builder strings.Builder
	for _, key := range keys {
		record := aggregated[key]
		builder.WriteString(record.Hostname)
		builder.WriteString(record.Type)
		builder.WriteString(fmt.Sprintf("%d", record.TTL))
		for _, value := range uniqueSorted(record.Values) {
			builder.WriteString(value)
		}
	}

	sum := sha1.Sum([]byte(builder.String()))
	serial := uint64(2026000000)
	for i := 0; i < 4; i++ {
		serial = serial*10 + uint64(sum[i]%10)
	}

	return fmt.Sprintf("%d", serial)
}

func sha1Hex(input string) string {
	sum := sha1.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}
