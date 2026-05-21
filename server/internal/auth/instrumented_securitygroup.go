package auth

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

// InstrumentedSecurityGroups decorates a SecurityGroupRepository with per-call
// observation. It preserves the operational visibility previously emitted by
// metrics.InstrumentedStore when these methods lived in db.Store.
type InstrumentedSecurityGroups struct {
	inner    SecurityGroupRepository
	observer Observer
}

// NewInstrumentedSecurityGroups wraps inner with metric observation.
func NewInstrumentedSecurityGroups(inner SecurityGroupRepository, observer Observer) *InstrumentedSecurityGroups {
	return &InstrumentedSecurityGroups{inner: inner, observer: observer}
}

func (i *InstrumentedSecurityGroups) Create(ctx context.Context, g *SecurityGroup) error {
	start := time.Now()
	err := i.inner.Create(ctx, g)
	i.observer.Observe("auth.SecurityGroup.Create", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedSecurityGroups) Get(ctx context.Context, id SecurityGroupID) (*SecurityGroup, error) {
	start := time.Now()
	g, err := i.inner.Get(ctx, id)
	i.observer.Observe("auth.SecurityGroup.Get", time.Since(start), err == nil)
	return g, err
}

func (i *InstrumentedSecurityGroups) List(ctx context.Context) ([]*SecurityGroup, error) {
	start := time.Now()
	gs, err := i.inner.List(ctx)
	i.observer.Observe("auth.SecurityGroup.List", time.Since(start), err == nil)
	return gs, err
}

func (i *InstrumentedSecurityGroups) Delete(ctx context.Context, id SecurityGroupID) error {
	start := time.Now()
	err := i.inner.Delete(ctx, id)
	i.observer.Observe("auth.SecurityGroup.Delete", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedSecurityGroups) AddMember(ctx context.Context, groupID SecurityGroupID, userID uuid.UUID) error {
	start := time.Now()
	err := i.inner.AddMember(ctx, groupID, userID)
	i.observer.Observe("auth.SecurityGroup.AddMember", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedSecurityGroups) RemoveMember(ctx context.Context, groupID SecurityGroupID, userID uuid.UUID) error {
	start := time.Now()
	err := i.inner.RemoveMember(ctx, groupID, userID)
	i.observer.Observe("auth.SecurityGroup.RemoveMember", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedSecurityGroups) ListMembers(ctx context.Context, groupID SecurityGroupID) ([]*Member, error) {
	start := time.Now()
	ms, err := i.inner.ListMembers(ctx, groupID)
	i.observer.Observe("auth.SecurityGroup.ListMembers", time.Since(start), err == nil)
	return ms, err
}

func (i *InstrumentedSecurityGroups) IsUserInGroup(ctx context.Context, userID uuid.UUID, groupID SecurityGroupID) (bool, error) {
	start := time.Now()
	ok, err := i.inner.IsUserInGroup(ctx, userID, groupID)
	i.observer.Observe("auth.SecurityGroup.IsUserInGroup", time.Since(start), err == nil)
	return ok, err
}

func (i *InstrumentedSecurityGroups) CountMembers(ctx context.Context, groupID SecurityGroupID) (int, error) {
	start := time.Now()
	n, err := i.inner.CountMembers(ctx, groupID)
	i.observer.Observe("auth.SecurityGroup.CountMembers", time.Since(start), err == nil)
	return n, err
}
