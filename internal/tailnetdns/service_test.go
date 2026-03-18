package tailnetdns

import (
	"context"
	"errors"
	"testing"
)

type fakeSplitDNSClient struct {
	getResult   map[string][]string
	patchResult map[string][]string
	getErr      error
	patchErr    error
	patches     []map[string]any
}

func (f *fakeSplitDNSClient) GetSplitDNS(context.Context) (map[string][]string, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeSplitDNSClient) PatchSplitDNS(_ context.Context, changes map[string]any) (map[string][]string, error) {
	f.patches = append(f.patches, changes)
	if f.patchErr != nil {
		return nil, f.patchErr
	}
	return f.patchResult, nil
}

func TestEnsureSplitDNSNoDrift(t *testing.T) {
	t.Parallel()

	client := &fakeSplitDNSClient{
		getResult: map[string][]string{
			"internal.example.test": {"100.70.110.111"},
		},
	}

	result, err := EnsureSplitDNS(context.Background(), client, "internal.example.test", "100.70.110.111")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DriftDetected {
		t.Fatal("expected no drift")
	}
	if result.Applied {
		t.Fatal("expected no patch call")
	}
	if len(client.patches) != 0 {
		t.Fatal("expected no patch requests")
	}
}

func TestEnsureSplitDNSRepairsDrift(t *testing.T) {
	t.Parallel()

	client := &fakeSplitDNSClient{
		getResult: map[string][]string{
			"internal.example.test": {"100.70.110.112"},
		},
		patchResult: map[string][]string{
			"internal.example.test": {"100.70.110.111"},
		},
	}

	result, err := EnsureSplitDNS(context.Background(), client, "internal.example.test", "100.70.110.111")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected repair patch")
	}
	if result.DriftDetected {
		t.Fatal("expected drift to be cleared after repair")
	}
	if result.ConfiguredNameserver != "100.70.110.111" {
		t.Fatalf("unexpected configured nameserver: %s", result.ConfiguredNameserver)
	}
}

func TestEnsureSplitDNSPatchFailure(t *testing.T) {
	t.Parallel()

	client := &fakeSplitDNSClient{
		getResult: map[string][]string{
			"internal.example.test": {"100.70.110.112"},
		},
		patchErr: errors.New("boom"),
	}

	_, err := EnsureSplitDNS(context.Background(), client, "internal.example.test", "100.70.110.111")
	if err == nil {
		t.Fatal("expected error")
	}
}
