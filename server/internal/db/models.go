package db

import (
	"time"

	"github.com/google/uuid"
)

// DeviceID uniquely identifies a device.
type DeviceID = uuid.UUID

// UserID uniquely identifies a user.
type UserID = uuid.UUID

// GroupID uniquely identifies a device group.
type GroupID = uuid.UUID

// DeviceStatus represents the connection state of a device.
type DeviceStatus string

const (
	StatusOnline     DeviceStatus = "online"
	StatusOffline    DeviceStatus = "offline"
	StatusConnecting DeviceStatus = "connecting"
)

// Device represents a managed device/agent.
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

// Group organizes devices for access control.
type Group struct {
	ID        GroupID   `json:"id"`
	Name      string    `json:"name"`
	OwnerID   UserID    `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// User represents an authenticated user of the system.
type User struct {
	ID           UserID    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"display_name"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AgentSession tracks an active relay session between browser and agent.
type AgentSession struct {
	Token     string    `json:"token"`
	DeviceID  DeviceID  `json:"device_id"`
	UserID    UserID    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

// WebPushSubscription stores a user's Web Push subscription.
type WebPushSubscription struct {
	Endpoint string `json:"endpoint"`
	UserID   UserID `json:"user_id"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// AMTDevice represents an Intel AMT device connected via CIRA.
type AMTDevice struct {
	UUID     uuid.UUID    `json:"uuid"`
	Hostname string       `json:"hostname"`
	Model    string       `json:"model"`
	Firmware string       `json:"firmware"`
	Status   DeviceStatus `json:"status"`
	LastSeen time.Time    `json:"last_seen"`
}

// NetworkInterfaceInfo describes a single network interface on the agent host.
type NetworkInterfaceInfo struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

// DeviceHardware stores hardware inventory for a device.
type DeviceHardware struct {
	DeviceID          DeviceID               `json:"device_id"`
	CPUModel          string                 `json:"cpu_model"`
	CPUCores          int                    `json:"cpu_cores"`
	RAMTotalMB        int64                  `json:"ram_total_mb"`
	DiskTotalMB       int64                  `json:"disk_total_mb"`
	DiskFreeMB        int64                  `json:"disk_free_mb"`
	NetworkInterfaces []NetworkInterfaceInfo  `json:"network_interfaces"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// DeviceLogEntry stores a single log entry fetched from a device.
type DeviceLogEntry struct {
	ID        int64     `json:"id"`
	DeviceID  DeviceID  `json:"device_id"`
	Timestamp string    `json:"timestamp"`
	Level     string    `json:"level"`
	Target    string    `json:"target"`
	Message   string    `json:"message"`
	FetchedAt time.Time `json:"fetched_at"`
}

// LogFilter specifies criteria for querying device logs.
type LogFilter struct {
	Level  string
	From   string
	To     string
	Search string
	Offset int
	Limit  int
}

