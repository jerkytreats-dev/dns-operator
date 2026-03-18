package tailnetdns

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"sort"
)

type EnsureResult struct {
	ConfiguredNameserver string
	DriftDetected        bool
	Applied              bool
}

func EnsureSplitDNS(ctx context.Context, client SplitDNSClient, zone, nameserver string) (EnsureResult, error) {
	if zone == "" {
		return EnsureResult{}, fmt.Errorf("zone cannot be empty")
	}
	if ip := net.ParseIP(nameserver); ip == nil {
		return EnsureResult{}, fmt.Errorf("nameserver must be a valid IP address")
	}

	current, err := client.GetSplitDNS(ctx)
	if err != nil {
		return EnsureResult{}, fmt.Errorf("get split dns: %w", err)
	}

	desired := []string{nameserver}
	currentValues := append([]string(nil), current[zone]...)
	sort.Strings(desired)
	sort.Strings(currentValues)

	result := EnsureResult{
		ConfiguredNameserver: firstOrEmpty(current[zone]),
		DriftDetected:        !reflect.DeepEqual(currentValues, desired),
	}

	if !result.DriftDetected {
		result.ConfiguredNameserver = nameserver
		return result, nil
	}

	updated, err := client.PatchSplitDNS(ctx, map[string]any{
		zone: []string{nameserver},
	})
	if err != nil {
		return result, fmt.Errorf("patch split dns: %w", err)
	}

	result.Applied = true
	result.ConfiguredNameserver = firstOrEmpty(updated[zone])
	result.DriftDetected = !containsExact(updated[zone], nameserver)
	return result, nil
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func containsExact(values []string, target string) bool {
	if len(values) != 1 {
		return false
	}
	return values[0] == target
}
