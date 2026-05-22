package auth

import (
	"context"
	"time"
)

// InstrumentedUsers decorates a UserRepository with per-call observation.
// It preserves the operational visibility previously emitted by
// metrics.InstrumentedStore when these methods lived in db.Store.
type InstrumentedUsers struct {
	inner    UserRepository
	observer Observer
}

// NewInstrumentedUsers wraps inner with metric observation.
func NewInstrumentedUsers(inner UserRepository, observer Observer) *InstrumentedUsers {
	return &InstrumentedUsers{inner: inner, observer: observer}
}

func (i *InstrumentedUsers) Upsert(ctx context.Context, u *User) error {
	start := time.Now()
	err := i.inner.Upsert(ctx, u)
	i.observer.Observe("auth.User.Upsert", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedUsers) Get(ctx context.Context, id UserID) (*User, error) {
	start := time.Now()
	u, err := i.inner.Get(ctx, id)
	i.observer.Observe("auth.User.Get", time.Since(start), err == nil)
	return u, err
}

func (i *InstrumentedUsers) GetByEmail(ctx context.Context, email string) (*User, error) {
	start := time.Now()
	u, err := i.inner.GetByEmail(ctx, email)
	i.observer.Observe("auth.User.GetByEmail", time.Since(start), err == nil)
	return u, err
}

func (i *InstrumentedUsers) List(ctx context.Context) ([]*User, error) {
	start := time.Now()
	users, err := i.inner.List(ctx)
	i.observer.Observe("auth.User.List", time.Since(start), err == nil)
	return users, err
}

func (i *InstrumentedUsers) Delete(ctx context.Context, id UserID) error {
	start := time.Now()
	err := i.inner.Delete(ctx, id)
	i.observer.Observe("auth.User.Delete", time.Since(start), err == nil)
	return err
}
