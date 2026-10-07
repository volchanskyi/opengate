// Package session owns the agent session aggregate and its Repository port.
package session

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrSessionNotFound is returned when a session token does not exist.
var ErrSessionNotFound = errors.New("agent session not found")

// Session tracks an active relay session between browser and agent.
type Session struct {
	Token     string    `json:"token"`
	DeviceID  uuid.UUID `json:"device_id"`
	UserID    uuid.UUID `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Repository is the outbound persistence port for agent sessions.
type Repository interface {
	Create(ctx context.Context, s *Session) error
	Get(ctx context.Context, token string) (*Session, error)
	Delete(ctx context.Context, token string) error
	// DeleteRelaySession removes one ended relay session by token under its own admin scope,
	// because relay teardown has no request tenant.
	DeleteRelaySession(ctx context.Context, token string) error
	ListActiveForDevice(ctx context.Context, deviceID uuid.UUID) ([]*Session, error)
	// DeleteStale removes every session created before cutoff whose token is absent from keep,
	// across all tenants, and returns the row count. It carries its own scope.
	DeleteStale(ctx context.Context, cutoff time.Time, keep []string) (int, error)
}
