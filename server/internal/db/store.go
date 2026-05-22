// Package db provides the database abstraction layer backed by PostgreSQL.
package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotFound indicates the requested record does not exist.
var ErrNotFound = errors.New("not found")

// Store defines the database operations for all persistent data.
type Store interface {
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

	// Health
	Ping(ctx context.Context) error
	Close() error
}
