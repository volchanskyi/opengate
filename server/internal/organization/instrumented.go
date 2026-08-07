package organization

import (
	"context"
	"time"
)

// Observer records the duration and success of a single repository call. The
// metrics package supplies a Prometheus-backed implementation; tests supply an
// in-memory recorder.
type Observer interface {
	Observe(operation string, duration time.Duration, ok bool)
}

// Instrumented decorates a Repository with per-call observation.
type Instrumented struct {
	inner    Repository
	observer Observer
}

// NewInstrumented wraps inner with metric observation.
func NewInstrumented(inner Repository, observer Observer) *Instrumented {
	return &Instrumented{inner: inner, observer: observer}
}

func (i *Instrumented) Create(ctx context.Context, org *Organization) error {
	start := time.Now()
	err := i.inner.Create(ctx, org)
	i.observer.Observe("organization.Create", time.Since(start), err == nil)
	return err
}

func (i *Instrumented) Get(ctx context.Context, id ID) (*Organization, error) {
	start := time.Now()
	org, err := i.inner.Get(ctx, id)
	i.observer.Observe("organization.Get", time.Since(start), err == nil)
	return org, err
}

func (i *Instrumented) List(ctx context.Context, includeArchived bool) ([]*Organization, error) {
	start := time.Now()
	orgs, err := i.inner.List(ctx, includeArchived)
	i.observer.Observe("organization.List", time.Since(start), err == nil)
	return orgs, err
}

func (i *Instrumented) Rename(ctx context.Context, id ID, name string) error {
	start := time.Now()
	err := i.inner.Rename(ctx, id, name)
	i.observer.Observe("organization.Rename", time.Since(start), err == nil)
	return err
}

func (i *Instrumented) SetArchived(ctx context.Context, id ID, archived bool) error {
	start := time.Now()
	err := i.inner.SetArchived(ctx, id, archived)
	i.observer.Observe("organization.SetArchived", time.Since(start), err == nil)
	return err
}

func (i *Instrumented) Delete(ctx context.Context, id ID) error {
	start := time.Now()
	err := i.inner.Delete(ctx, id)
	i.observer.Observe("organization.Delete", time.Since(start), err == nil)
	return err
}

func (i *Instrumented) EnsureDefault(ctx context.Context) (ID, error) {
	start := time.Now()
	id, err := i.inner.EnsureDefault(ctx)
	i.observer.Observe("organization.EnsureDefault", time.Since(start), err == nil)
	return id, err
}
