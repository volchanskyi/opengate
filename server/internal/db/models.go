package db

import (
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/device"
)

// DeviceID uniquely identifies a device.
type DeviceID = uuid.UUID

// UserID uniquely identifies a user.
type UserID = uuid.UUID

// GroupID uniquely identifies a device group.
type GroupID = uuid.UUID

// DeviceStatus is aliased to the canonical device-aggregate type so existing
// db.StatusOnline / db.StatusOffline / db.StatusConnecting references in
// not-yet-extracted modules (AMT, integration tests) keep compiling while the
// type itself now lives in [device]. Removed once those callers migrate to
// device.StatusX directly.
type DeviceStatus = device.DeviceStatus

const (
	StatusOnline     = device.StatusOnline
	StatusOffline    = device.StatusOffline
	StatusConnecting = device.StatusConnecting
)

// Compat aliases for the not-yet-extracted callers that still spell these
// types with a `db.` prefix. Test files, bench files, AMT code, etc. — every
// alias here is a direct rename to its canonical home in [device]. Removed
// once the matching modules complete their ADR-021 extraction.
type (
	Device               = device.Device
	Group                = device.Group
	DeviceHardware       = device.Hardware
	DeviceLogEntry       = device.LogEntry
	LogFilter            = device.LogFilter
	NetworkInterfaceInfo = device.NetworkInterfaceInfo
)

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
