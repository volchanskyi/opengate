// Package session owns the agent session aggregate (browser ↔ relay ↔ agent
// session tokens). Per ADR-021, the Repository outbound port and its types
// live with the consuming module; the Postgres adapter lives alongside in
// postgres.go.
package session

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrSessionNotFound is returned when a Get / Delete targets a session token
// that does not exist.
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
	ListActiveForDevice(ctx context.Context, deviceID uuid.UUID) ([]*Session, error)
}
