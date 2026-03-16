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
	AgentVersion string       `json:"agent_version"`
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

// AuditEvent records a security-relevant action.
type AuditEvent struct {
	ID        int64     `json:"id"`
	UserID    UserID    `json:"user_id"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditQuery specifies filters for querying the audit log.
type AuditQuery struct {
	UserID *UserID
	Action string
	Limit  int
	Offset int
}
