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

func (i *InstrumentedDevices) List(ctx context.Context, filter Filter) ([]*Device, error) {
	return observe1(i.observer, "device.Device.List", func() ([]*Device, error) { return i.inner.List(ctx, filter) })
}

func (i *InstrumentedDevices) GetByAMTUUID(ctx context.Context, amtUUID uuid.UUID) (*Device, error) {
	return observe1(i.observer, "device.Device.GetByAMTUUID", func() (*Device, error) { return i.inner.GetByAMTUUID(ctx, amtUUID) })
}

func (i *InstrumentedDevices) Delete(ctx context.Context, id DeviceID) error {
	return observe0(i.observer, "device.Device.Delete", func() error { return i.inner.Delete(ctx, id) })
}

func (i *InstrumentedDevices) UpdateSite(ctx context.Context, id DeviceID, siteID SiteID) error {
	return observe0(i.observer, "device.Device.UpdateSite", func() error { return i.inner.UpdateSite(ctx, id, siteID) })
}

func (i *InstrumentedDevices) SetStatus(ctx context.Context, id DeviceID, status DeviceStatus) error {
	return observe0(i.observer, "device.Device.SetStatus", func() error { return i.inner.SetStatus(ctx, id, status) })
}

func (i *InstrumentedDevices) ResetAllStatuses(ctx context.Context) error {
	return observe0(i.observer, "device.Device.ResetAllStatuses", func() error { return i.inner.ResetAllStatuses(ctx) })
}

func (i *InstrumentedDevices) SetMaintenance(ctx context.Context, id DeviceID, on bool, by uuid.UUID, reason string) error {
	return observe0(i.observer, "device.Device.SetMaintenance", func() error { return i.inner.SetMaintenance(ctx, id, on, by, reason) })
}

func (i *InstrumentedDevices) Counts(ctx context.Context, organizationID OrganizationID) (Counts, error) {
	return observe1(i.observer, "device.Device.Counts", func() (Counts, error) { return i.inner.Counts(ctx, organizationID) })
}

func (i *InstrumentedDevices) UpdateOrganization(ctx context.Context, id DeviceID, organizationID OrganizationID) error {
	return observe0(i.observer, "device.Device.UpdateOrganization", func() error {
		return i.inner.UpdateOrganization(ctx, id, organizationID)
	})
}

// InstrumentedSites decorates a SiteRepository with per-call observation.
type InstrumentedSites struct {
	inner    SiteRepository
	observer Observer
}

// NewInstrumentedSites wraps inner with metric observation.
func NewInstrumentedSites(inner SiteRepository, observer Observer) *InstrumentedSites {
	return &InstrumentedSites{inner: inner, observer: observer}
}

func (i *InstrumentedSites) Create(ctx context.Context, s *Site) error {
	return observe0(i.observer, "device.Site.Create", func() error { return i.inner.Create(ctx, s) })
}

func (i *InstrumentedSites) Get(ctx context.Context, id SiteID) (*Site, error) {
	return observe1(i.observer, "device.Site.Get", func() (*Site, error) { return i.inner.Get(ctx, id) })
}

func (i *InstrumentedSites) List(ctx context.Context, organizationID OrganizationID) ([]*Site, error) {
	return observe1(i.observer, "device.Site.List", func() ([]*Site, error) { return i.inner.List(ctx, organizationID) })
}

func (i *InstrumentedSites) Delete(ctx context.Context, id SiteID) error {
	return observe0(i.observer, "device.Site.Delete", func() error { return i.inner.Delete(ctx, id) })
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

func (i *InstrumentedHardware) ResolveBySystemUUID(ctx context.Context, systemUUID uuid.UUID) (DeviceID, uuid.UUID, error) {
	start := time.Now()
	deviceID, tenantID, err := i.inner.ResolveBySystemUUID(ctx, systemUUID)
	i.observer.Observe("device.Hardware.ResolveBySystemUUID", time.Since(start), err == nil)
	return deviceID, tenantID, err
}

func (i *InstrumentedHardware) SetAMTDetail(ctx context.Context, deviceID DeviceID, model, firmware string) error {
	return observe0(i.observer, "device.Hardware.SetAMTDetail", func() error {
		return i.inner.SetAMTDetail(ctx, deviceID, model, firmware)
	})
}
