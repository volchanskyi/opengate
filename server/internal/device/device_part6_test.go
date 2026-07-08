package device_test

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/device"
)

func (m *memDevices) Delete(_ context.Context, _ device.DeviceID) error { return m.maybeFail() }

func (m *memDevices) UpdateGroup(_ context.Context, _ device.DeviceID, _ device.GroupID) error {
	return m.maybeFail()
}

func (m *memDevices) SetStatus(_ context.Context, _ device.DeviceID, _ device.DeviceStatus) error {
	return m.maybeFail()
}

func (m *memDevices) ResetAllStatuses(_ context.Context) error { return m.maybeFail() }

type memGroups struct{ failEvery bool }

func (m *memGroups) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}

func (m *memGroups) Create(_ context.Context, _ *device.Group) error { return m.maybeFail() }

func (m *memGroups) Get(_ context.Context, _ device.GroupID) (*device.Group, error) {
	return &device.Group{}, m.maybeFail()
}

func (m *memGroups) List(_ context.Context, _ uuid.UUID) ([]*device.Group, error) {
	return nil, m.maybeFail()
}

func (m *memGroups) Delete(_ context.Context, _ device.GroupID) error { return m.maybeFail() }

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
