package notifications

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Observer records the duration and success of a single repository call.
// The metrics package supplies a Prometheus-backed implementation; tests
// supply an in-memory recorder.
type Observer interface {
	Observe(operation string, duration time.Duration, ok bool)
}

// InstrumentedWebPush decorates a WebPushRepository with per-call observation.
// It preserves the operational visibility previously emitted by
// metrics.InstrumentedStore when these methods lived in db.Store.
type InstrumentedWebPush struct {
	inner    WebPushRepository
	observer Observer
}

// NewInstrumentedWebPush wraps inner with metric observation.
func NewInstrumentedWebPush(inner WebPushRepository, observer Observer) *InstrumentedWebPush {
	return &InstrumentedWebPush{inner: inner, observer: observer}
}

func (i *InstrumentedWebPush) Upsert(ctx context.Context, sub *WebPushSubscription) error {
	start := time.Now()
	err := i.inner.Upsert(ctx, sub)
	i.observer.Observe("notifications.WebPush.Upsert", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedWebPush) ListForUser(ctx context.Context, userID uuid.UUID) ([]*WebPushSubscription, error) {
	start := time.Now()
	subs, err := i.inner.ListForUser(ctx, userID)
	i.observer.Observe("notifications.WebPush.ListForUser", time.Since(start), err == nil)
	return subs, err
}

func (i *InstrumentedWebPush) ListAll(ctx context.Context) ([]*WebPushSubscription, error) {
	start := time.Now()
	subs, err := i.inner.ListAll(ctx)
	i.observer.Observe("notifications.WebPush.ListAll", time.Since(start), err == nil)
	return subs, err
}

func (i *InstrumentedWebPush) Delete(ctx context.Context, endpoint string) error {
	start := time.Now()
	err := i.inner.Delete(ctx, endpoint)
	i.observer.Observe("notifications.WebPush.Delete", time.Since(start), err == nil)
	return err
}
