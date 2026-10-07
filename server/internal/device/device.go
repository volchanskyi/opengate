// Package device holds managed devices, sites, the hardware inventory and their repository ports.
package device

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// DeviceID uniquely identifies a device; it aliases uuid.UUID.
type DeviceID = uuid.UUID

// SiteID uniquely identifies a site.
type SiteID = uuid.UUID

// OrganizationID uniquely identifies the customer a device belongs to.
type OrganizationID = uuid.UUID

// DeviceStatus is the wire-protocol connection state of a managed device.
type DeviceStatus string

const (
	StatusOnline     DeviceStatus = "online"
	StatusOffline    DeviceStatus = "offline"
	StatusConnecting DeviceStatus = "connecting"
)

// ErrDeviceNotFound is returned when a Repository operation names an unknown device.
var ErrDeviceNotFound = errors.New("device not found")

// ErrSiteNotFound is returned when a SiteRepository operation names an unknown site.
var ErrSiteNotFound = errors.New("site not found")

// ErrSiteNameTaken is returned when a customer already has a site by that name.
// Site names are unique within one customer, not across the tenant.
var ErrSiteNameTaken = errors.New("site name already used by this customer")

// ErrSiteNotInOrganization is returned when a device is filed into another customer's site.
var ErrSiteNotInOrganization = errors.New("site does not belong to the device's organization")

// ErrHardwareNotFound is returned when no hardware inventory exists for a device.
var ErrHardwareNotFound = errors.New("device hardware not found")

// ErrOrganizationNotFound is returned when a move names a customer absent from the device's tenant.
// Foreign-key checks bypass row-level security, so this check keeps devices inside their tenant.
var ErrOrganizationNotFound = errors.New("organization not found in this tenant")

// Filter narrows a device list. A zero field matches everything, and set fields narrow together.
type Filter struct {
	SiteID         SiteID
	OrganizationID OrganizationID
}

// Device is a managed agent installation.
type Device struct {
	ID DeviceID `json:"id"`
	// OrganizationID is never zero on a stored device: a write naming none takes the tenant's own.
	OrganizationID OrganizationID `json:"organization_id"`
	SiteID         SiteID         `json:"site_id"`
	Hostname       string         `json:"hostname"`
	OS             string         `json:"os"`
	OsDisplay      string         `json:"os_display"`
	AgentVersion   string         `json:"agent_version"`
	Capabilities   []string       `json:"capabilities"`
	Status         DeviceStatus   `json:"status"`
	LastSeen       time.Time      `json:"last_seen"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`

	// The Maintenance fields hold the administrator-set suppression state; leaving it clears the rest.
	MaintenanceOn     bool       `json:"maintenance_on"`
	MaintenanceSince  *time.Time `json:"maintenance_since,omitempty"`
	MaintenanceBy     *uuid.UUID `json:"maintenance_by,omitempty"`
	MaintenanceReason string     `json:"maintenance_reason,omitempty"`

	// AMT is nil when the device has neither AMT support nor an AMT connection.
	AMT *AMT `json:"amt,omitempty"`
}

// Site is a location or department inside one customer.
// The tenant scopes visibility, so every member of a tenant sees every site in it.
type Site struct {
	ID             SiteID         `json:"id"`
	OrganizationID OrganizationID `json:"organization_id"`
	Name           string         `json:"name"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// NetworkInterfaceInfo is a single NIC reported by the agent's inventory.
type NetworkInterfaceInfo struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

// Hardware is the device hardware inventory. The agent report and the server's WSMAN query
// write disjoint columns (AMTModel and AMTFirmware belong to WSMAN).
type Hardware struct {
	DeviceID          DeviceID               `json:"device_id"`
	CPUModel          string                 `json:"cpu_model"`
	CPUCores          int                    `json:"cpu_cores"`
	RAMTotalMB        int64                  `json:"ram_total_mb"`
	DiskTotalMB       int64                  `json:"disk_total_mb"`
	DiskFreeMB        int64                  `json:"disk_free_mb"`
	NetworkInterfaces []NetworkInterfaceInfo `json:"network_interfaces"`
	UpdatedAt         time.Time              `json:"updated_at"`

	// SystemUUID is the SMBIOS UUID that links an AMT CIRA connection; the API never returns it.
	SystemUUID *uuid.UUID `json:"-"`
	// AMTAvailable nil keeps the stored value; a non-nil false is a stated absence and overwrites it.
	AMTAvailable *bool  `json:"amt_available,omitempty"`
	AMTVersion   string `json:"amt_version,omitempty"`
	AMTModel     string `json:"amt_model,omitempty"`
	AMTFirmware  string `json:"amt_firmware,omitempty"`
}

// AMT is a device's Intel AMT support and, when linked, its CIRA connection state.
type AMT struct {
	// Available mirrors the agent's Management Engine reading and is independent of connection state.
	Available bool `json:"available"`
	// Status is empty when no AMT connection has dialled in for this device.
	Status string `json:"status,omitempty"`
	// UUID is the CIRA identity that power actions address, nil when unlinked.
	UUID *uuid.UUID `json:"uuid,omitempty"`
}

// LogEntry is one raw log line streamed from an agent to the caller and never stored centrally.
type LogEntry struct {
	DeviceID  DeviceID `json:"device_id"`
	Timestamp string   `json:"timestamp"`
	Level     string   `json:"level"`
	Target    string   `json:"target"`
	Message   string   `json:"message"`
}

// LogFilter narrows an on-demand raw-log pull from the agent.
type LogFilter struct {
	Level  string
	From   string
	To     string
	Search string
	Offset int
	Limit  int
	// Source is "self" or empty for the agent's own files, "host" for the platform system log.
	Source string
	// Unit narrows host logs to one systemd unit; empty matches every unit.
	Unit string
}

// Repository is the persistence port for devices.
type Repository interface {
	Upsert(ctx context.Context, d *Device) error
	Get(ctx context.Context, id DeviceID) (*Device, error)
	// GetByAMTUUID resolves the device behind an AMT CIRA identity within the caller's tenant.
	// The AMT connection map carries no tenant, so this lookup scopes AMT commands to it.
	GetByAMTUUID(ctx context.Context, amtUUID uuid.UUID) (*Device, error)
	TenantForDevice(ctx context.Context, id DeviceID) (uuid.UUID, error)
	// List returns the caller's tenant devices matching filter; an empty filter matches all.
	List(ctx context.Context, filter Filter) ([]*Device, error)
	Delete(ctx context.Context, id DeviceID) error
	// UpdateSite files a device into a site, or unfiles it when siteID is zero.
	// A site of another customer returns ErrSiteNotInOrganization.
	UpdateSite(ctx context.Context, id DeviceID, siteID SiteID) error
	// UpdateOrganization moves a device to another customer in its tenant and unfiles its site.
	// Returns ErrDeviceNotFound or ErrOrganizationNotFound when either is out of scope.
	UpdateOrganization(ctx context.Context, id DeviceID, organizationID OrganizationID) error
	SetStatus(ctx context.Context, id DeviceID, status DeviceStatus) error
	ResetAllStatuses(ctx context.Context) error
	// SetMaintenance stamps entry time (kept on reason edits), user and reason; disabling clears all.
	// Returns ErrDeviceNotFound when no device in the tenant scope matches id.
	SetMaintenance(ctx context.Context, id DeviceID, on bool, by uuid.UUID, reason string) error
	// Counts returns the status rollup from one aggregate row; a zero organizationID counts the tenant.
	Counts(ctx context.Context, organizationID OrganizationID) (Counts, error)
}

// Counts is the fleet status rollup behind the dashboard tiles.
type Counts struct {
	Total int
	// Online excludes connecting devices.
	Online      int
	Maintenance int
}

// SiteRepository is the persistence port for sites.
type SiteRepository interface {
	// Create returns ErrOrganizationNotFound for a customer outside the caller's tenant.
	// It returns ErrSiteNameTaken when the customer already has that name.
	Create(ctx context.Context, s *Site) error
	Get(ctx context.Context, id SiteID) (*Site, error)
	// List returns the caller's sites, narrowed to one customer unless organizationID is zero.
	List(ctx context.Context, organizationID OrganizationID) ([]*Site, error)
	Delete(ctx context.Context, id SiteID) error
}

// HardwareRepository is the persistence port for the per-device hardware inventory.
type HardwareRepository interface {
	Upsert(ctx context.Context, hw *Hardware) error
	Get(ctx context.Context, deviceID DeviceID) (*Hardware, error)
	// ResolveBySystemUUID maps a system UUID to its device and tenant across all tenants.
	// No match, or several from cloned disks, is ErrHardwareNotFound.
	ResolveBySystemUUID(ctx context.Context, systemUUID uuid.UUID) (DeviceID, uuid.UUID, error)
	// SetAMTDetail writes the WSMAN model and firmware columns and leaves agent-sourced columns alone.
	// Returns ErrHardwareNotFound when no hardware row for deviceID exists in scope.
	SetAMTDetail(ctx context.Context, deviceID DeviceID, model, firmware string) error
}
