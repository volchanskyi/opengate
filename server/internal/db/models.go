package db

import (
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/session"
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

// AgentSession is aliased to the canonical type in [session] for the
// migration window while not-yet-updated callers still spell this with a
// `db.` prefix. Removed once those callers migrate to session.Session
// directly.
type AgentSession = session.Session

// WebPushSubscription is aliased to the canonical type in [notifications] for
// the migration window while the not-yet-updated callers still spell this
// with a `db.` prefix. Removed once those callers migrate to the
// notifications.WebPushSubscription identifier directly.
type WebPushSubscription = notifications.WebPushSubscription

// AMTDevice represents an Intel AMT device connected via CIRA.
type AMTDevice struct {
	UUID     uuid.UUID    `json:"uuid"`
	Hostname string       `json:"hostname"`
	Model    string       `json:"model"`
	Firmware string       `json:"firmware"`
	Status   DeviceStatus `json:"status"`
	LastSeen time.Time    `json:"last_seen"`
}
