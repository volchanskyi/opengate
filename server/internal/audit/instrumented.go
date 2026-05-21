package audit

import (
	"context"
	"time"
)

// Observer records the duration and success of a single Repository call.
// The api/metrics package supplies a Prometheus-backed implementation; tests
// supply an in-memory recorder.
type Observer interface {
	Observe(operation string, duration time.Duration, ok bool)
}

// Instrumented decorates a Repository with per-call observation. It preserves
// the operational visibility previously emitted by metrics.InstrumentedStore
// when audit lived in db.Store.
type Instrumented struct {
	inner    Repository
	observer Observer
}

// NewInstrumented wraps inner with metric observation through observer.
func NewInstrumented(inner Repository, observer Observer) *Instrumented {
	return &Instrumented{inner: inner, observer: observer}
}

func (i *Instrumented) Write(ctx context.Context, event *Event) error {
	start := time.Now()
	err := i.inner.Write(ctx, event)
	i.observer.Observe("audit.Write", time.Since(start), err == nil)
	return err
}

func (i *Instrumented) Query(ctx context.Context, q Query) ([]*Event, error) {
	start := time.Now()
	events, err := i.inner.Query(ctx, q)
	i.observer.Observe("audit.Query", time.Since(start), err == nil)
	return events, err
}
