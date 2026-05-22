package device

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Observer records the duration and success of a single repository call.
// metrics.Metrics supplies a Prometheus-backed implementation; tests supply
// an in-memory recorder.
type Observer interface {
	Observe(operation string, duration time.Duration, ok bool)
}

// --- Repository decorator ---------------------------------------------------

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
	start := time.Now()
	err := i.inner.Upsert(ctx, d)
	i.observer.Observe("device.Device.Upsert", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedDevices) Get(ctx context.Context, id DeviceID) (*Device, error) {
	start := time.Now()
	d, err := i.inner.Get(ctx, id)
	i.observer.Observe("device.Device.Get", time.Since(start), err == nil)
	return d, err
}

func (i *InstrumentedDevices) List(ctx context.Context, groupID GroupID) ([]*Device, error) {
	start := time.Now()
	ds, err := i.inner.List(ctx, groupID)
	i.observer.Observe("device.Device.List", time.Since(start), err == nil)
	return ds, err
}

func (i *InstrumentedDevices) ListAll(ctx context.Context) ([]*Device, error) {
	start := time.Now()
	ds, err := i.inner.ListAll(ctx)
	i.observer.Observe("device.Device.ListAll", time.Since(start), err == nil)
	return ds, err
}

func (i *InstrumentedDevices) ListForOwner(ctx context.Context, ownerID uuid.UUID) ([]*Device, error) {
	start := time.Now()
	ds, err := i.inner.ListForOwner(ctx, ownerID)
	i.observer.Observe("device.Device.ListForOwner", time.Since(start), err == nil)
	return ds, err
}

func (i *InstrumentedDevices) Delete(ctx context.Context, id DeviceID) error {
	start := time.Now()
	err := i.inner.Delete(ctx, id)
	i.observer.Observe("device.Device.Delete", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedDevices) UpdateGroup(ctx context.Context, id DeviceID, groupID GroupID) error {
	start := time.Now()
	err := i.inner.UpdateGroup(ctx, id, groupID)
	i.observer.Observe("device.Device.UpdateGroup", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedDevices) SetStatus(ctx context.Context, id DeviceID, status DeviceStatus) error {
	start := time.Now()
	err := i.inner.SetStatus(ctx, id, status)
	i.observer.Observe("device.Device.SetStatus", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedDevices) ResetAllStatuses(ctx context.Context) error {
	start := time.Now()
	err := i.inner.ResetAllStatuses(ctx)
	i.observer.Observe("device.Device.ResetAllStatuses", time.Since(start), err == nil)
	return err
}

// --- GroupRepository decorator ----------------------------------------------

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
	start := time.Now()
	err := i.inner.Create(ctx, g)
	i.observer.Observe("device.Group.Create", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedGroups) Get(ctx context.Context, id GroupID) (*Group, error) {
	start := time.Now()
	g, err := i.inner.Get(ctx, id)
	i.observer.Observe("device.Group.Get", time.Since(start), err == nil)
	return g, err
}

func (i *InstrumentedGroups) List(ctx context.Context, ownerID uuid.UUID) ([]*Group, error) {
	start := time.Now()
	gs, err := i.inner.List(ctx, ownerID)
	i.observer.Observe("device.Group.List", time.Since(start), err == nil)
	return gs, err
}

func (i *InstrumentedGroups) Delete(ctx context.Context, id GroupID) error {
	start := time.Now()
	err := i.inner.Delete(ctx, id)
	i.observer.Observe("device.Group.Delete", time.Since(start), err == nil)
	return err
}

// --- HardwareRepository decorator -------------------------------------------

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
	start := time.Now()
	err := i.inner.Upsert(ctx, hw)
	i.observer.Observe("device.Hardware.Upsert", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedHardware) Get(ctx context.Context, deviceID DeviceID) (*Hardware, error) {
	start := time.Now()
	hw, err := i.inner.Get(ctx, deviceID)
	i.observer.Observe("device.Hardware.Get", time.Since(start), err == nil)
	return hw, err
}

// --- LogsRepository decorator -----------------------------------------------

// InstrumentedLogs decorates a LogsRepository with per-call observation.
type InstrumentedLogs struct {
	inner    LogsRepository
	observer Observer
}

// NewInstrumentedLogs wraps inner with metric observation.
func NewInstrumentedLogs(inner LogsRepository, observer Observer) *InstrumentedLogs {
	return &InstrumentedLogs{inner: inner, observer: observer}
}

func (i *InstrumentedLogs) Upsert(ctx context.Context, deviceID DeviceID, entries []LogEntry) error {
	start := time.Now()
	err := i.inner.Upsert(ctx, deviceID, entries)
	i.observer.Observe("device.Logs.Upsert", time.Since(start), err == nil)
	return err
}

func (i *InstrumentedLogs) Query(ctx context.Context, deviceID DeviceID, filter LogFilter) ([]LogEntry, int, error) {
	start := time.Now()
	entries, total, err := i.inner.Query(ctx, deviceID, filter)
	i.observer.Observe("device.Logs.Query", time.Since(start), err == nil)
	return entries, total, err
}

func (i *InstrumentedLogs) HasRecent(ctx context.Context, deviceID DeviceID, maxAge time.Duration) (bool, error) {
	start := time.Now()
	ok, err := i.inner.HasRecent(ctx, deviceID, maxAge)
	i.observer.Observe("device.Logs.HasRecent", time.Since(start), err == nil)
	return ok, err
}
