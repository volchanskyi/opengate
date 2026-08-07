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

	// Maintenance mode: the server-authoritative desired suppression state an
	// administrator toggles. MaintenanceOn defaults to false (Active). When set,
	// MaintenanceSince/MaintenanceBy/MaintenanceReason record when it was entered,
	// which user set it, and why; all three are cleared on exit.
	MaintenanceOn     bool       `json:"maintenance_on"`
	MaintenanceSince  *time.Time `json:"maintenance_since,omitempty"`
	MaintenanceBy     *uuid.UUID `json:"maintenance_by,omitempty"`
	MaintenanceReason string     `json:"maintenance_reason,omitempty"`

	// AMT is the device's Intel AMT property, read alongside the device row.
	// Nil when the device neither supports AMT nor has an AMT connection.
	AMT *AMT `json:"amt,omitempty"`
}

// Group is a named collection of devices within one organization. It is a
// filing label, not an access boundary: the organization is what scopes
// visibility, so every member of an organization sees every group in it.
type Group struct {
	ID        GroupID   `json:"id"`
	Name      string    `json:"name"`
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
//
// Two writers share the row. The agent's inventory report owns the host facts
// plus SystemUUID / AMTAvailable / AMTVersion; the server's WSMAN query over a
// CIRA connection owns AMTModel / AMTFirmware. Every write is column-targeted so
// neither writer blanks the other's columns.
type Hardware struct {
	DeviceID          DeviceID               `json:"device_id"`
	CPUModel          string                 `json:"cpu_model"`
	CPUCores          int                    `json:"cpu_cores"`
	RAMTotalMB        int64                  `json:"ram_total_mb"`
	DiskTotalMB       int64                  `json:"disk_total_mb"`
	DiskFreeMB        int64                  `json:"disk_free_mb"`
	NetworkInterfaces []NetworkInterfaceInfo `json:"network_interfaces"`
	UpdatedAt         time.Time              `json:"updated_at"`

	// SystemUUID is the host's SMBIOS system UUID, which an Intel AMT CIRA
	// connection presents as its own identity. It is a join key only: stored to
	// resolve that link, never returned by the API, and preserved when an agent
	// report omits it.
	SystemUUID *uuid.UUID `json:"-"`
	// AMTAvailable reports whether the host exposes a Management Engine
	// interface. Nil means the agent said nothing, which preserves the stored
	// value; a non-nil false is a stated absence and overwrites it.
	AMTAvailable *bool  `json:"amt_available,omitempty"`
	AMTVersion   string `json:"amt_version,omitempty"`
	AMTModel     string `json:"amt_model,omitempty"`
	AMTFirmware  string `json:"amt_firmware,omitempty"`
}

// AMT is a managed device's Intel AMT property: whether the hardware supports
// AMT at all, and the state of its CIRA connection when one is linked.
type AMT struct {
	// Available mirrors the agent's Management Engine reading. It drives the
	// badge on its own, so the badge never flickers with connection state.
	Available bool `json:"available"`
	// Status is the CIRA connection state, empty when no AMT connection has ever
	// dialled in for this device.
	Status string `json:"status,omitempty"`
	// UUID is the AMT device's CIRA identity, nil when unlinked. Power actions
	// address the device by this value.
	UUID *uuid.UUID `json:"uuid,omitempty"`
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
	// Source selects the host log source: "self"/"" for the agent's own files,
	// "host" to auto-resolve the platform system log (journald on Linux).
	Source string
	// Unit narrows host logs to one emitting unit (systemd unit); empty matches
	// every unit.
	Unit string
}

// Repository is the outbound persistence port for the Device aggregate root.
type Repository interface {
	Upsert(ctx context.Context, d *Device) error
	Get(ctx context.Context, id DeviceID) (*Device, error)
	// GetByAMTUUID resolves the managed device behind an Intel AMT CIRA
	// identity. The connection map that serves power commands is keyed by that
	// UUID alone and carries no tenant, so this tenant-scoped lookup is what
	// keeps an AMT command inside the caller's organization. Returns
	// ErrDeviceNotFound when no device in scope owns the UUID.
	GetByAMTUUID(ctx context.Context, amtUUID uuid.UUID) (*Device, error)
	OrgForDevice(ctx context.Context, id DeviceID) (uuid.UUID, error)
	List(ctx context.Context, groupID GroupID) ([]*Device, error)
	ListAll(ctx context.Context) ([]*Device, error)
	Delete(ctx context.Context, id DeviceID) error
	UpdateGroup(ctx context.Context, id DeviceID, groupID GroupID) error
	SetStatus(ctx context.Context, id DeviceID, status DeviceStatus) error
	ResetAllStatuses(ctx context.Context) error
	// SetMaintenance toggles a device's maintenance state. Enabling stamps the
	// entry time (preserved across an in-place reason edit), the acting user, and
	// the reason; disabling clears all three. Returns ErrDeviceNotFound when no
	// device in the current tenant scope matches id.
	SetMaintenance(ctx context.Context, id DeviceID, on bool, by uuid.UUID, reason string) error
	// Counts returns the fleet status rollup for the current tenant scope in one
	// aggregate row, so the dashboard never reads device rows to count them.
	Counts(ctx context.Context) (Counts, error)
}

// Counts is the fleet status rollup behind the dashboard tiles: three integers
// from one aggregate row, whatever the fleet size.
type Counts struct {
	// Total is every device in the organization.
	Total int
	// Online is the devices with a live agent connection. A connecting device
	// is not online, which matches how the tiles present it.
	Online int
	// Maintenance is the devices currently in maintenance mode.
	Maintenance int
}

// GroupRepository is the outbound persistence port for device groups.
type GroupRepository interface {
	Create(ctx context.Context, g *Group) error
	Get(ctx context.Context, id GroupID) (*Group, error)
	List(ctx context.Context) ([]*Group, error)
	Delete(ctx context.Context, id GroupID) error
}

// HardwareRepository is the outbound persistence port for the per-device
// hardware inventory.
type HardwareRepository interface {
	Upsert(ctx context.Context, hw *Hardware) error
	Get(ctx context.Context, deviceID DeviceID) (*Hardware, error)
	// ResolveBySystemUUID maps an SMBIOS system UUID to the device that reported
	// it and that device's organization. An Intel AMT CIRA connection arrives
	// with no request tenant, so this lookup supplies its own admin scope and
	// searches across organizations; the organization it returns is what scopes
	// every write that follows. Returns ErrHardwareNotFound when no device
	// matches, and likewise when several do — a UUID shared by cloned disk
	// images is not an identity.
	ResolveBySystemUUID(ctx context.Context, systemUUID uuid.UUID) (DeviceID, uuid.UUID, error)
	// SetAMTDetail writes the machine model and AMT firmware version read over
	// WSMAN, leaving every agent-sourced column untouched. Returns
	// ErrHardwareNotFound when no hardware row for deviceID exists in scope.
	SetAMTDetail(ctx context.Context, deviceID DeviceID, model, firmware string) error
}
