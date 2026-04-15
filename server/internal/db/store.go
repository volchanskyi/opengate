// Package db provides the database abstraction layer backed by PostgreSQL.
package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound indicates the requested record does not exist.
var ErrNotFound = errors.New("not found")

// Store defines the database operations for all persistent data.
type Store interface {
	// Devices
	UpsertDevice(ctx context.Context, d *Device) error
	GetDevice(ctx context.Context, id DeviceID) (*Device, error)
	ListDevices(ctx context.Context, groupID GroupID) ([]*Device, error)
	ListAllDevices(ctx context.Context) ([]*Device, error)
	ListDevicesForOwner(ctx context.Context, ownerID UserID) ([]*Device, error)
	DeleteDevice(ctx context.Context, id DeviceID) error
	UpdateDeviceGroup(ctx context.Context, id DeviceID, groupID GroupID) error
	SetDeviceStatus(ctx context.Context, id DeviceID, status DeviceStatus) error
	ResetAllDeviceStatuses(ctx context.Context) error

	// Groups
	CreateGroup(ctx context.Context, g *Group) error
	GetGroup(ctx context.Context, id GroupID) (*Group, error)
	ListGroups(ctx context.Context, ownerID UserID) ([]*Group, error)
	DeleteGroup(ctx context.Context, id GroupID) error

	// Users
	UpsertUser(ctx context.Context, u *User) error
	GetUser(ctx context.Context, id UserID) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	ListUsers(ctx context.Context) ([]*User, error)
	DeleteUser(ctx context.Context, id UserID) error

	// Agent Sessions
	CreateAgentSession(ctx context.Context, s *AgentSession) error
	GetAgentSession(ctx context.Context, token string) (*AgentSession, error)
	DeleteAgentSession(ctx context.Context, token string) error
	ListActiveSessionsForDevice(ctx context.Context, deviceID DeviceID) ([]*AgentSession, error)

	// Web Push
	UpsertWebPushSubscription(ctx context.Context, sub *WebPushSubscription) error
	ListWebPushSubscriptions(ctx context.Context, userID UserID) ([]*WebPushSubscription, error)
	ListAllWebPushSubscriptions(ctx context.Context) ([]*WebPushSubscription, error)
	DeleteWebPushSubscription(ctx context.Context, endpoint string) error

	// AMT Devices
	UpsertAMTDevice(ctx context.Context, d *AMTDevice) error
	GetAMTDevice(ctx context.Context, id uuid.UUID) (*AMTDevice, error)
	ListAMTDevices(ctx context.Context) ([]*AMTDevice, error)
	SetAMTDeviceStatus(ctx context.Context, id uuid.UUID, status DeviceStatus) error

	// Enrollment Tokens
	CreateEnrollmentToken(ctx context.Context, t *EnrollmentToken) error
	GetEnrollmentTokenByToken(ctx context.Context, token string) (*EnrollmentToken, error)
	ListEnrollmentTokens(ctx context.Context, createdBy UserID) ([]*EnrollmentToken, error)
	DeleteEnrollmentToken(ctx context.Context, id uuid.UUID) error
	IncrementEnrollmentTokenUseCount(ctx context.Context, id uuid.UUID) error

	// Audit
	WriteAuditEvent(ctx context.Context, event *AuditEvent) error
	QueryAuditLog(ctx context.Context, q AuditQuery) ([]*AuditEvent, error)

	// Device Hardware
	UpsertDeviceHardware(ctx context.Context, hw *DeviceHardware) error
	GetDeviceHardware(ctx context.Context, deviceID DeviceID) (*DeviceHardware, error)

	// Device Logs
	UpsertDeviceLogs(ctx context.Context, deviceID DeviceID, entries []DeviceLogEntry) error
	QueryDeviceLogs(ctx context.Context, deviceID DeviceID, filter LogFilter) ([]DeviceLogEntry, int, error)
	HasRecentLogs(ctx context.Context, deviceID DeviceID, maxAge time.Duration) (bool, error)

	// Device Updates
	CreateDeviceUpdate(ctx context.Context, du *DeviceUpdate) error
	UpdateDeviceUpdateStatus(ctx context.Context, deviceID DeviceID, version string, status UpdateStatus, errMsg string) error
	ListDeviceUpdatesByVersion(ctx context.Context, version string) ([]*DeviceUpdate, error)

	// Security Groups
	CreateSecurityGroup(ctx context.Context, g *SecurityGroup) error
	GetSecurityGroup(ctx context.Context, id SecurityGroupID) (*SecurityGroup, error)
	ListSecurityGroups(ctx context.Context) ([]*SecurityGroup, error)
	DeleteSecurityGroup(ctx context.Context, id SecurityGroupID) error
	AddSecurityGroupMember(ctx context.Context, groupID SecurityGroupID, userID UserID) error
	RemoveSecurityGroupMember(ctx context.Context, groupID SecurityGroupID, userID UserID) error
	ListSecurityGroupMembers(ctx context.Context, groupID SecurityGroupID) ([]*User, error)
	IsUserInSecurityGroup(ctx context.Context, userID UserID, groupID SecurityGroupID) (bool, error)
	CountSecurityGroupMembers(ctx context.Context, groupID SecurityGroupID) (int, error)

	// Health
	Ping(ctx context.Context) error
	Close() error
}
