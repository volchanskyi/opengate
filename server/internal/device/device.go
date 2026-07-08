// Package device owns the device aggregate: managed devices and
// their groupings, plus the hardware-inventory read model that hangs off the
// same aggregate root. The outbound persistence ports (Repository /
// GroupRepository / HardwareRepository) live here; their Postgres adapters live
// alongside in postgres.go and the Instrumented decorators in instrumented.go.
package device

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// DeviceID and GroupID alias uuid.UUID so callers passing a uuid.UUID get the
// right name in the type system without an extra conversion.
type DeviceID = uuid.UUID

// GroupID uniquely identifies a device group.
type GroupID = uuid.UUID

// DeviceStatus is the wire-protocol connection state of a managed device.
type DeviceStatus string

// DeviceStatus values.
const (
	StatusOnline     DeviceStatus = "online"
	StatusOffline    DeviceStatus = "offline"
	StatusConnecting DeviceStatus = "connecting"
)

// ErrDeviceNotFound is returned by Repository ops on an unknown device id.
var ErrDeviceNotFound = errors.New("device not found")

// ErrGroupNotFound is returned by GroupRepository ops on an unknown group id.
var ErrGroupNotFound = errors.New("group not found")

// ErrHardwareNotFound is returned when no hardware inventory exists for a device.
var ErrHardwareNotFound = errors.New("device hardware not found")

// Device is a managed agent installation.
type Device struct {
	ID           DeviceID     `json:"id"`
	GroupID      GroupID      `json:"group_id"`
	Hostname     string       `json:"hostname"`
	OS           string       `json:"os"`
	OsDisplay    string       `json:"os_display"`
	AgentVersion string       `json:"agent_version"`
	Capabilities []string     `json:"capabilities"`
	Status       DeviceStatus `json:"status"`
	LastSeen     time.Time    `json:"last_seen"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// Group is a named collection of devices that share an owner for access control.
type Group struct {
	ID        GroupID   `json:"id"`
	Name      string    `json:"name"`
	OwnerID   uuid.UUID `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NetworkInterfaceInfo is a single NIC reported by the agent's inventory.
type NetworkInterfaceInfo struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

// Hardware is the device-hardware-inventory read model.
type Hardware struct {
	DeviceID          DeviceID               `json:"device_id"`
	CPUModel          string                 `json:"cpu_model"`
	CPUCores          int                    `json:"cpu_cores"`
	RAMTotalMB        int64                  `json:"ram_total_mb"`
	DiskTotalMB       int64                  `json:"disk_total_mb"`
	DiskFreeMB        int64                  `json:"disk_free_mb"`
	NetworkInterfaces []NetworkInterfaceInfo `json:"network_interfaces"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// LogEntry is a single raw log line brokered on-demand from a device. It is
// transient — streamed from agent to caller and never persisted centrally.
type LogEntry struct {
	DeviceID  DeviceID `json:"device_id"`
	Timestamp string   `json:"timestamp"`
	Level     string   `json:"level"`
	Target    string   `json:"target"`
	Message   string   `json:"message"`
}

// LogFilter narrows an on-demand raw-log pull brokered to the agent.
type LogFilter struct {
	Level  string
	From   string
	To     string
	Search string
	Offset int
	Limit  int
}

// Repository is the outbound persistence port for the Device aggregate root.
type Repository interface {
	Upsert(ctx context.Context, d *Device) error
	Get(ctx context.Context, id DeviceID) (*Device, error)
	OrgForDevice(ctx context.Context, id DeviceID) (uuid.UUID, error)
	List(ctx context.Context, groupID GroupID) ([]*Device, error)
	ListAll(ctx context.Context) ([]*Device, error)
	ListForOwner(ctx context.Context, ownerID uuid.UUID) ([]*Device, error)
	Delete(ctx context.Context, id DeviceID) error
	UpdateGroup(ctx context.Context, id DeviceID, groupID GroupID) error
	SetStatus(ctx context.Context, id DeviceID, status DeviceStatus) error
	ResetAllStatuses(ctx context.Context) error
}

// GroupRepository is the outbound persistence port for device groups.
type GroupRepository interface {
	Create(ctx context.Context, g *Group) error
	Get(ctx context.Context, id GroupID) (*Group, error)
	List(ctx context.Context, ownerID uuid.UUID) ([]*Group, error)
	Delete(ctx context.Context, id GroupID) error
}

// HardwareRepository is the outbound persistence port for the per-device
// hardware inventory.
type HardwareRepository interface {
	Upsert(ctx context.Context, hw *Hardware) error
	Get(ctx context.Context, deviceID DeviceID) (*Hardware, error)
}
