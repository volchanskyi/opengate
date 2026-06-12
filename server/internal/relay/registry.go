package relay

import (
	"context"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// SessionMeta is the persisted metadata for a session in the registry. The live
// Conn pair stays in-process on the owning server; a future distributed adapter
// would persist this so peers can read it.
type SessionMeta struct {
	CreatedAt     time.Time
	ExpectedSides []Side
	ServerID      string
}

// SessionRegistry is the outbound port for session tracking. It is the retained
// seam for a future multi-server relay pool; today a single in-process adapter
// satisfies it:
//
//   - InProcessRegistry — single-server deployments. Returned by
//     NewInProcessRegistry.
//
// The relay code is agnostic about which adapter is in use, so a future
// distributed adapter can be added without changing relay behavior.
type SessionRegistry interface {
	// SaveSession persists session metadata, creating the entry owned by
	// meta.ServerID if none exists. Idempotent — a repeat call leaves an
	// existing entry untouched.
	SaveSession(ctx context.Context, token protocol.SessionToken, meta SessionMeta) error

	// DeleteSession removes the session entry. A no-op if the token has no entry.
	DeleteSession(ctx context.Context, token protocol.SessionToken) error

	// Ping reports whether the registry is ready. The in-process adapter has no
	// external dependency and always returns nil.
	Ping(ctx context.Context) error
}
