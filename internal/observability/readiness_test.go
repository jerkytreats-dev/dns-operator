package observability

import (
	"context"
	"testing"
	"time"
)

func TestCacheSyncReadinessTransitionsToReady(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	readiness := NewCacheSyncReadiness(func(context.Context) bool {
		started <- struct{}{}
		return true
	})
	if err := readiness.Check(nil); err == nil {
		t.Fatal("expected readiness check to fail before cache sync")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- readiness.Start(ctx)
	}()

	<-started
	var readyErr error
	for range 10 {
		readyErr = readiness.Check(nil)
		if readyErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if readyErr != nil {
		t.Fatalf("expected readiness check to succeed after cache sync, got %v", readyErr)
	}

	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatalf("unexpected start error: %v", err)
	}
}
