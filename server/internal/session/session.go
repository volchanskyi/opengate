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

// ErrDeviceHeld is returned when another technician holds the device's one session.
var ErrDeviceHeld = errors.New("another technician holds a session on this device")

// Session tracks an active relay session between browser and agent; a device holds at most one.
type Session struct {
	Token     string    `json:"token"`
	DeviceID  uuid.UUID `json:"device_id"`
	UserID    uuid.UUID `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	// HolderName is the holder's display name, or their email when they have none; reads fill it.
	HolderName string `json:"holder_name"`
}

// Repository is the outbound persistence port for agent sessions.
type Repository interface {
	// Create records s as its device's one session. A session the same user holds there is
	// replaced and its token returned; ErrDeviceHeld when another user holds the device.
	Create(ctx context.Context, s *Session) (replaced string, err error)
	Get(ctx context.Context, token string) (*Session, error)
	// ForDevice returns the device's one session, or ErrSessionNotFound when none is open.
	ForDevice(ctx context.Context, deviceID uuid.UUID) (*Session, error)
	// MarkOutcome sets the session's mark for one direct-link outcome, numbered from 0, and
	// reports whether this call set it.
	MarkOutcome(ctx context.Context, token string, outcome int) (bool, error)
	Delete(ctx context.Context, token string) error
	// DeleteRelaySession removes one ended relay session by token under its own admin scope,
	// because relay teardown has no request tenant.
	DeleteRelaySession(ctx context.Context, token string) error
	ListActiveForDevice(ctx context.Context, deviceID uuid.UUID) ([]*Session, error)
	// DeleteStale removes every session created before cutoff whose token is absent from keep,
	// across all tenants, and returns the row count. It carries its own scope.
	DeleteStale(ctx context.Context, cutoff time.Time, keep []string) (int, error)
}
