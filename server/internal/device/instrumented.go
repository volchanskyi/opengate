package device

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Observer records the duration and success of a single repository call.
type Observer interface {
	Observe(operation string, duration time.Duration, ok bool)
}

// observe0 times a no-result repository call and reports it to the observer.
func observe0(o Observer, op string, fn func() error) error {
	start := time.Now()
	err := fn()
	o.Observe(op, time.Since(start), err == nil)
	return err
}

// observe1 times a single-result repository call and reports it to the observer.
func observe1[T any](o Observer, op string, fn func() (T, error)) (T, error) {
	start := time.Now()
	v, err := fn()
	o.Observe(op, time.Since(start), err == nil)
	return v, err
}

// InstrumentedDevices decorates a Repository with per-call observation.
type InstrumentedDevices struct {
	inner    Repository
	observer Observer
}

// NewInstrumentedDevices wraps inner with metric observation.
func NewInstrumentedDevices(inner Repository, observer Observer) *InstrumentedDevices {
	return &InstrumentedDevices{inner: inner, observer: observer}
}

func (i *InstrumentedDevices) Upsert(ctx context.Context, d *Device) error {
	return observe0(i.observer, "device.Device.Upsert", func() error { return i.inner.Upsert(ctx, d) })
}

func (i *InstrumentedDevices) Get(ctx context.Context, id DeviceID) (*Device, error) {
	return observe1(i.observer, "device.Device.Get", func() (*Device, error) { return i.inner.Get(ctx, id) })
}

func (i *InstrumentedDevices) List(ctx context.Context, groupID GroupID) ([]*Device, error) {
	return observe1(i.observer, "device.Device.List", func() ([]*Device, error) { return i.inner.List(ctx, groupID) })
}

func (i *InstrumentedDevices) ListAll(ctx context.Context) ([]*Device, error) {
	return observe1(i.observer, "device.Device.ListAll", func() ([]*Device, error) { return i.inner.ListAll(ctx) })
}

func (i *InstrumentedDevices) ListForOwner(ctx context.Context, ownerID uuid.UUID) ([]*Device, error) {
	return observe1(i.observer, "device.Device.ListForOwner", func() ([]*Device, error) { return i.inner.ListForOwner(ctx, ownerID) })
}

func (i *InstrumentedDevices) Delete(ctx context.Context, id DeviceID) error {
	return observe0(i.observer, "device.Device.Delete", func() error { return i.inner.Delete(ctx, id) })
}

func (i *InstrumentedDevices) UpdateGroup(ctx context.Context, id DeviceID, groupID GroupID) error {
	return observe0(i.observer, "device.Device.UpdateGroup", func() error { return i.inner.UpdateGroup(ctx, id, groupID) })
}

func (i *InstrumentedDevices) SetStatus(ctx context.Context, id DeviceID, status DeviceStatus) error {
	return observe0(i.observer, "device.Device.SetStatus", func() error { return i.inner.SetStatus(ctx, id, status) })
}

func (i *InstrumentedDevices) ResetAllStatuses(ctx context.Context) error {
	return observe0(i.observer, "device.Device.ResetAllStatuses", func() error { return i.inner.ResetAllStatuses(ctx) })
}

// InstrumentedGroups decorates a GroupRepository with per-call observation.
type InstrumentedGroups struct {
	inner    GroupRepository
	observer Observer
}

// NewInstrumentedGroups wraps inner with metric observation.
func NewInstrumentedGroups(inner GroupRepository, observer Observer) *InstrumentedGroups {
	return &InstrumentedGroups{inner: inner, observer: observer}
}

func (i *InstrumentedGroups) Create(ctx context.Context, g *Group) error {
	return observe0(i.observer, "device.Group.Create", func() error { return i.inner.Create(ctx, g) })
}

func (i *InstrumentedGroups) Get(ctx context.Context, id GroupID) (*Group, error) {
	return observe1(i.observer, "device.Group.Get", func() (*Group, error) { return i.inner.Get(ctx, id) })
}

func (i *InstrumentedGroups) List(ctx context.Context, ownerID uuid.UUID) ([]*Group, error) {
	return observe1(i.observer, "device.Group.List", func() ([]*Group, error) { return i.inner.List(ctx, ownerID) })
}

func (i *InstrumentedGroups) Delete(ctx context.Context, id GroupID) error {
	return observe0(i.observer, "device.Group.Delete", func() error { return i.inner.Delete(ctx, id) })
}

// InstrumentedHardware decorates a HardwareRepository with per-call observation.
type InstrumentedHardware struct {
	inner    HardwareRepository
	observer Observer
}

// NewInstrumentedHardware wraps inner with metric observation.
func NewInstrumentedHardware(inner HardwareRepository, observer Observer) *InstrumentedHardware {
	return &InstrumentedHardware{inner: inner, observer: observer}
}

func (i *InstrumentedHardware) Upsert(ctx context.Context, hw *Hardware) error {
	return observe0(i.observer, "device.Hardware.Upsert", func() error { return i.inner.Upsert(ctx, hw) })
}

func (i *InstrumentedHardware) Get(ctx context.Context, deviceID DeviceID) (*Hardware, error) {
	return observe1(i.observer, "device.Hardware.Get", func() (*Hardware, error) { return i.inner.Get(ctx, deviceID) })
}
