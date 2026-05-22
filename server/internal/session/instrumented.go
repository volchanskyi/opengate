package session

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

func (i *Instrumented) Create(ctx context.Context, s *Session) error {
	start := time.Now()
	err := i.inner.Create(ctx, s)
	i.observer.Observe("session.Create", time.Since(start), err == nil)
	return err
}

func (i *Instrumented) Get(ctx context.Context, token string) (*Session, error) {
	start := time.Now()
	s, err := i.inner.Get(ctx, token)
	i.observer.Observe("session.Get", time.Since(start), err == nil)
	return s, err
}

func (i *Instrumented) Delete(ctx context.Context, token string) error {
	start := time.Now()
	err := i.inner.Delete(ctx, token)
	i.observer.Observe("session.Delete", time.Since(start), err == nil)
	return err
}

func (i *Instrumented) ListActiveForDevice(ctx context.Context, deviceID uuid.UUID) ([]*Session, error) {
	start := time.Now()
	sessions, err := i.inner.ListActiveForDevice(ctx, deviceID)
	i.observer.Observe("session.ListActiveForDevice", time.Since(start), err == nil)
	return sessions, err
}
