package relay

import (
	"context"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// SessionMeta is the metadata for a session in the registry; the live Conn pair stays in-process.
type SessionMeta struct {
	CreatedAt     time.Time
	ExpectedSides []Side
	ServerID      string
}

// SessionRegistry is the outbound port for session tracking; InProcessRegistry implements it.
type SessionRegistry interface {
	// SaveSession creates the entry owned by meta.ServerID; a repeat call leaves it untouched.
	SaveSession(ctx context.Context, token protocol.SessionToken, meta SessionMeta) error

	// DeleteSession removes the session entry. A no-op if the token has no entry.
	DeleteSession(ctx context.Context, token protocol.SessionToken) error

	// Ping reports whether the registry is ready.
	Ping(ctx context.Context) error
}
