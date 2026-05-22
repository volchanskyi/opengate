// Package db provides the database abstraction layer backed by PostgreSQL.
package db

import (
	"context"
	"errors"
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

	// Health
	Ping(ctx context.Context) error
	Close() error
}
