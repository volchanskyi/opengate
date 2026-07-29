package amt

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// Observer records the duration and success of a single repository call.
// The metrics package supplies a Prometheus-backed implementation; tests
// supply an in-memory recorder.
type Observer interface {
	Observe(operation string, duration time.Duration, ok bool)
}

// Instrumented decorates a Repository with per-call observation. It preserves
// the operational visibility previously emitted by metrics.InstrumentedStore
// when these methods lived in db.Store.
type Instrumented struct {
	inner    Repository
	observer Observer
}

// NewInstrumented wraps inner with metric observation.
func NewInstrumented(inner Repository, observer Observer) *Instrumented {
	return &Instrumented{inner: inner, observer: observer}
}

func (i *Instrumented) Upsert(ctx context.Context, d *db.AMTDevice) error {
	start := time.Now()
	err := i.inner.Upsert(ctx, d)
	i.observer.Observe("amt.Upsert", time.Since(start), err == nil)
	return err
}

func (i *Instrumented) SetStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error {
	start := time.Now()
	err := i.inner.SetStatus(ctx, id, status)
	i.observer.Observe("amt.SetStatus", time.Since(start), err == nil)
	return err
}
