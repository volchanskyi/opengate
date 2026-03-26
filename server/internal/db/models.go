package db

import (
	"errors"
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

// EnrollmentToken authorises agent enrollment and CA certificate retrieval.
type EnrollmentToken struct {
	ID        uuid.UUID `json:"id"`
	Token     string    `json:"token"`
	Label     string    `json:"label"`
	CreatedBy UserID    `json:"created_by"`
	MaxUses   int       `json:"max_uses"`
	UseCount  int       `json:"use_count"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
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

// UpdateStatus represents the outcome of a pushed update.
type UpdateStatus string

const (
	UpdateStatusPending UpdateStatus = "pending"
	UpdateStatusSuccess UpdateStatus = "success"
	UpdateStatusFailed  UpdateStatus = "failed"
)

// DeviceUpdate tracks a single update push to a device.
type DeviceUpdate struct {
	ID       int64        `json:"id"`
	DeviceID DeviceID     `json:"device_id"`
	Version  string       `json:"version"`
	Status   UpdateStatus `json:"status"`
	Error    string       `json:"error"`
	PushedAt time.Time    `json:"pushed_at"`
	AckedAt  *time.Time   `json:"acked_at,omitempty"`
}

// SecurityGroupID uniquely identifies a security group.
type SecurityGroupID = uuid.UUID

// AdminGroupID is the well-known UUID for the built-in Administrators group.
var AdminGroupID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// ErrSystemGroup is returned when attempting to delete a system group.
var ErrSystemGroup = errors.New("cannot delete system group")

// ErrLastAdmin is returned when attempting to remove the last member of the Administrators group.
var ErrLastAdmin = errors.New("cannot remove last administrator")

// SecurityGroup represents a named permission group.
type SecurityGroup struct {
	ID          SecurityGroupID `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	IsSystem    bool            `json:"is_system"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// SecurityGroupMember represents a user's membership in a security group.
type SecurityGroupMember struct {
	GroupID SecurityGroupID `json:"group_id"`
	UserID  UserID          `json:"user_id"`
	AddedAt time.Time       `json:"added_at"`
}
