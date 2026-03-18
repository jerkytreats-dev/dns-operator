package validation

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

const DefaultAuthoritativeZone = "internal.example.test"

var hostnamePattern = regexp.MustCompile(`^([a-z0-9]([-a-z0-9]*[a-z0-9])?\.)+[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type ZonePolicy struct {
	publishZones      []string
	authoritativeZone string
}

func NewZonePolicy(publishZones []string, authoritativeZone string) (ZonePolicy, error) {
	normalizedAuthoritativeZone, err := normalizeZone(authoritativeZone)
	if err != nil {
		return ZonePolicy{}, fmt.Errorf("authoritative zone: %w", err)
	}

	normalizedPublishZones := make([]string, 0, len(publishZones))
	seen := map[string]struct{}{}
	for _, zone := range publishZones {
		normalizedZone, err := normalizeZone(zone)
		if err != nil {
			return ZonePolicy{}, fmt.Errorf("publish zone %q: %w", zone, err)
		}
		if _, found := seen[normalizedZone]; found {
			continue
		}
		seen[normalizedZone] = struct{}{}
		normalizedPublishZones = append(normalizedPublishZones, normalizedZone)
	}
	if len(normalizedPublishZones) == 0 {
		normalizedPublishZones = []string{normalizedAuthoritativeZone}
	}
	sort.Strings(normalizedPublishZones)

	return ZonePolicy{
		publishZones:      normalizedPublishZones,
		authoritativeZone: normalizedAuthoritativeZone,
	}, nil
}

func MustNewZonePolicy(publishZones []string, authoritativeZone string) ZonePolicy {
	policy, err := NewZonePolicy(publishZones, authoritativeZone)
	if err != nil {
		panic(err)
	}
	return policy
}

func DefaultZonePolicy() ZonePolicy {
	return MustNewZonePolicy([]string{DefaultAuthoritativeZone}, DefaultAuthoritativeZone)
}

func (p ZonePolicy) PublishZones() []string {
	return append([]string(nil), p.publishZones...)
}

func (p ZonePolicy) AuthoritativeZone() string {
	return p.authoritativeZone
}

func (p ZonePolicy) ValidatePublishedHostname(hostname string) error {
	if err := ValidateFQDN(hostname); err != nil {
		return err
	}
	for _, zone := range p.publishZones {
		if HostnameInZone(hostname, zone) {
			return nil
		}
	}
	return fmt.Errorf("hostname must be within one of %s", strings.Join(p.publishZones, ", "))
}

func (p ZonePolicy) ValidateAuthoritativeHostname(hostname string) error {
	if err := ValidateFQDN(hostname); err != nil {
		return err
	}
	if !HostnameInZone(hostname, p.authoritativeZone) {
		return fmt.Errorf("hostname must be within authoritative zone %s", p.authoritativeZone)
	}
	return nil
}

func (p ZonePolicy) IsAuthoritativeHostname(hostname string) bool {
	return p.ValidateAuthoritativeHostname(hostname) == nil
}

func (p ZonePolicy) RelativeName(hostname string) (string, error) {
	if err := p.ValidateAuthoritativeHostname(hostname); err != nil {
		return "", err
	}
	if hostname == p.authoritativeZone {
		return "@", nil
	}
	return strings.TrimSuffix(hostname, "."+p.authoritativeZone), nil
}

func HostnameInZone(hostname, zone string) bool {
	hostname = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(hostname, ".")))
	zone = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(zone, ".")))
	return hostname == zone || strings.HasSuffix(hostname, "."+zone)
}

func ValidateManagedHostname(hostname string) error {
	return DefaultZonePolicy().ValidateAuthoritativeHostname(hostname)
}

func ValidateFQDN(hostname string) error {
	if hostname == "" {
		return fmt.Errorf("hostname cannot be empty")
	}

	if hostname != strings.ToLower(hostname) {
		return fmt.Errorf("hostname must be lowercase")
	}

	if strings.HasSuffix(hostname, ".") {
		return fmt.Errorf("hostname must not end with a dot")
	}

	if !hostnamePattern.MatchString(hostname) {
		return fmt.Errorf("hostname is not a valid DNS name")
	}

	return nil
}

func InferRecordFromAddress(address string) (string, string, error) {
	if address == "" {
		return "", "", fmt.Errorf("address cannot be empty")
	}

	if ip := net.ParseIP(address); ip != nil {
		if ip.To4() != nil {
			return "A", address, nil
		}
		return "AAAA", address, nil
	}

	if err := ValidateFQDN(address); err != nil {
		return "", "", fmt.Errorf("address must be an IP or FQDN: %w", err)
	}

	return "CNAME", address, nil
}

func RelativeName(hostname string) string {
	relativeName, err := DefaultZonePolicy().RelativeName(hostname)
	if err != nil {
		return hostname
	}
	return relativeName
}

func normalizeZone(zone string) (string, error) {
	normalizedZone := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(zone, ".")))
	if normalizedZone == "" {
		return "", fmt.Errorf("zone cannot be empty")
	}
	if err := ValidateFQDN(normalizedZone); err != nil {
		return "", err
	}
	return normalizedZone, nil
}
