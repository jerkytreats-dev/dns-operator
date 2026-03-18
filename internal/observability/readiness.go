package observability

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
)

type CacheSyncReadiness struct {
	waitForSync func(context.Context) bool
	ready       atomic.Bool
}

func NewCacheSyncReadiness(waitForSync func(context.Context) bool) *CacheSyncReadiness {
	return &CacheSyncReadiness{waitForSync: waitForSync}
}

func (r *CacheSyncReadiness) Start(ctx context.Context) error {
	if r.waitForSync == nil {
		r.ready.Store(true)
		<-ctx.Done()
		return nil
	}

	if !r.waitForSync(ctx) {
		return ctx.Err()
	}

	r.ready.Store(true)
	<-ctx.Done()
	return nil
}

func (r *CacheSyncReadiness) Check(_ *http.Request) error {
	if r.ready.Load() {
		return nil
	}
	return errors.New("controller cache has not finished syncing")
}
