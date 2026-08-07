package device_test

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/device"
)

func (m *memDevices) Delete(_ context.Context, _ device.DeviceID) error { return m.maybeFail() }

func (m *memDevices) UpdateSite(_ context.Context, _ device.DeviceID, _ device.SiteID) error {
	return m.maybeFail()
}

func (m *memDevices) SetStatus(_ context.Context, _ device.DeviceID, _ device.DeviceStatus) error {
	return m.maybeFail()
}

func (m *memDevices) ResetAllStatuses(_ context.Context) error { return m.maybeFail() }

func (m *memDevices) SetMaintenance(_ context.Context, _ device.DeviceID, _ bool, _ uuid.UUID, _ string) error {
	return m.maybeFail()
}

func (m *memDevices) Counts(_ context.Context, _ device.OrganizationID) (device.Counts, error) {
	return device.Counts{}, m.maybeFail()
}

func (m *memDevices) GetByAMTUUID(_ context.Context, _ uuid.UUID) (*device.Device, error) {
	return &device.Device{}, m.maybeFail()
}

type memSites struct{ failEvery bool }

func (m *memSites) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}

func (m *memSites) Create(_ context.Context, _ *device.Site) error { return m.maybeFail() }

func (m *memSites) Get(_ context.Context, _ device.SiteID) (*device.Site, error) {
	return &device.Site{}, m.maybeFail()
}

func (m *memSites) List(_ context.Context, _ device.OrganizationID) ([]*device.Site, error) {
	return nil, m.maybeFail()
}

func (m *memSites) Delete(_ context.Context, _ device.SiteID) error { return m.maybeFail() }

type memHardware struct{ failEvery bool }

func (m *memHardware) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}

func (m *memHardware) Upsert(_ context.Context, _ *device.Hardware) error { return m.maybeFail() }

func (m *memHardware) Get(_ context.Context, _ device.DeviceID) (*device.Hardware, error) {
	return &device.Hardware{}, m.maybeFail()
}

func (m *memHardware) ResolveBySystemUUID(_ context.Context, _ uuid.UUID) (device.DeviceID, uuid.UUID, error) {
	return uuid.Nil, uuid.Nil, m.maybeFail()
}

func (m *memHardware) SetAMTDetail(_ context.Context, _ device.DeviceID, _, _ string) error {
	return m.maybeFail()
}
