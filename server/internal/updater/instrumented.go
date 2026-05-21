package updater

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

// --- DeviceUpdate decorator ---

// InstrumentedDeviceUpdates decorates a DeviceUpdateRepository with per-call
// observation. It preserves the operational visibility previously emitted by
// metrics.InstrumentedStore when these methods lived in db.Store.
type InstrumentedDeviceUpdates struct {
	inner    DeviceUpdateRepository
	observer Observer
}

// NewInstrumentedDeviceUpdates wraps inner with metric observation.
func NewInstrumentedDeviceUpdates(inner DeviceUpdateRepository, observer Observer) *InstrumentedDeviceUpdates {
	return &InstrumentedDeviceUpdates{inner: inner, observer: observer}
}

func (i *InstrumentedDeviceUpdates) Create(ctx context.Context, du *DeviceUpdate) error {
	start := time.Now()
	err := i.inner.Create(ctx, du)
	i.observer.Observe("updater.DeviceUpdate.Create", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedDeviceUpdates) SetStatus(ctx context.Context, deviceID uuid.UUID, version string, status Status, errMsg string) error {
	start := time.Now()
	err := i.inner.SetStatus(ctx, deviceID, version, status, errMsg)
	i.observer.Observe("updater.DeviceUpdate.SetStatus", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedDeviceUpdates) ListByVersion(ctx context.Context, version string) ([]*DeviceUpdate, error) {
	start := time.Now()
	out, err := i.inner.ListByVersion(ctx, version)
	i.observer.Observe("updater.DeviceUpdate.ListByVersion", time.Since(start), err == nil)
	return out, err
}

// --- Enrollment decorator ---

// InstrumentedEnrollment decorates an EnrollmentTokenRepository with per-call
// observation.
type InstrumentedEnrollment struct {
	inner    EnrollmentTokenRepository
	observer Observer
}

// NewInstrumentedEnrollment wraps inner with metric observation.
func NewInstrumentedEnrollment(inner EnrollmentTokenRepository, observer Observer) *InstrumentedEnrollment {
	return &InstrumentedEnrollment{inner: inner, observer: observer}
}

func (i *InstrumentedEnrollment) Create(ctx context.Context, t *EnrollmentToken) error {
	start := time.Now()
	err := i.inner.Create(ctx, t)
	i.observer.Observe("updater.Enrollment.Create", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedEnrollment) GetByToken(ctx context.Context, token string) (*EnrollmentToken, error) {
	start := time.Now()
	out, err := i.inner.GetByToken(ctx, token)
	i.observer.Observe("updater.Enrollment.GetByToken", time.Since(start), err == nil)
	return out, err
}

func (i *InstrumentedEnrollment) List(ctx context.Context, createdBy uuid.UUID) ([]*EnrollmentToken, error) {
	start := time.Now()
	out, err := i.inner.List(ctx, createdBy)
	i.observer.Observe("updater.Enrollment.List", time.Since(start), err == nil)
	return out, err
}

func (i *InstrumentedEnrollment) Delete(ctx context.Context, id uuid.UUID) error {
	start := time.Now()
	err := i.inner.Delete(ctx, id)
	i.observer.Observe("updater.Enrollment.Delete", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedEnrollment) IncrementUseCount(ctx context.Context, id uuid.UUID) error {
	start := time.Now()
	err := i.inner.IncrementUseCount(ctx, id)
	i.observer.Observe("updater.Enrollment.IncrementUseCount", time.Since(start), err == nil)
	return err
}
