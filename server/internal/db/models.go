package db

import (
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/session"
)

// DeviceID uniquely identifies a device.
type DeviceID = uuid.UUID

// UserID uniquely identifies a user.
type UserID = uuid.UUID

// SiteID uniquely identifies a device site.
type SiteID = uuid.UUID

// DeviceStatus aliases the device aggregate's status type for callers using the db prefix.
type DeviceStatus = device.DeviceStatus

const (
	StatusOnline     = device.StatusOnline
	StatusOffline    = device.StatusOffline
	StatusConnecting = device.StatusConnecting
)

// These aliases name the device package's types for callers using the db prefix.
type (
	Device               = device.Device
	Site                 = device.Site
	DeviceHardware       = device.Hardware
	DeviceLogEntry       = device.LogEntry
	LogFilter            = device.LogFilter
	NetworkInterfaceInfo = device.NetworkInterfaceInfo
)

// User aliases auth.User for callers using the db prefix.
type User = auth.User

// AgentSession aliases session.Session for callers using the db prefix.
type AgentSession = session.Session

// WebPushSubscription aliases notifications.WebPushSubscription for callers using the db prefix.
type WebPushSubscription = notifications.WebPushSubscription

// AMTDevice represents an Intel AMT device connected via CIRA.
type AMTDevice struct {
	// UUID is the AMT firmware's CIRA identity, which on vPro hardware equals the
	// host's SMBIOS system UUID.
	UUID uuid.UUID `json:"uuid"`
	// DeviceID is the managed device resolved from the system UUID; a connection without one is
	// held in memory only.
	DeviceID uuid.UUID    `json:"device_id"`
	Status   DeviceStatus `json:"status"`
	LastSeen time.Time    `json:"last_seen"`
}
