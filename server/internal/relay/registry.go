package relay

import (
	"context"
	"errors"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// ErrRegistryNotFound is returned by SessionRegistry operations when the
// requested token has no registry entry.
var ErrRegistryNotFound = errors.New("session registry: not found")

// ErrInvalidArgument is returned when a SessionRegistry argument is rejected
// at the boundary (empty serverID, nil token, etc.).
var ErrInvalidArgument = errors.New("session registry: invalid argument")

// EventKind enumerates the session-lifecycle events broadcast through
// SessionRegistry.SubscribeEvents.
type EventKind int

const (
	// EventCreated fires when a session token is first registered with the
	// owning server.
	EventCreated EventKind = iota
	// EventSideJoined fires when one of the two relay sides registers.
	EventSideJoined
	// EventEnded fires when a session ends — either both sides disconnect
	// or the owning server reclaims due to TTL expiry.
	EventEnded
)

// SessionEvent is a lifecycle event broadcast by the owning server to any
// peer servers that have subscribed.
type SessionEvent struct {
	Kind     EventKind
	Token    protocol.SessionToken
	ServerID string
	// Side is populated for EventSideJoined; ignored otherwise.
	Side *Side
}

// SessionMeta is the persisted metadata for a session in the registry —
// the bits another server needs to know about a session it does not own.
// The live Conn pair stays in-process on the owning server.
type SessionMeta struct {
	CreatedAt     time.Time
	ExpectedSides []Side
	ServerID      string
}

// SessionRegistry is the outbound port for distributed session-affinity
// tracking across a relay pool.
//
// Two adapters satisfy this port:
//
//  1. InProcessRegistry — single-server deployments. Returned by
//     NewInProcessRegistry.
//  2. RedisRegistry — multi-server deployments with shared affinity state.
//
// The relay code is agnostic about which adapter is in use; deployment
// configuration selects the adapter without changing relay behavior.
type SessionRegistry interface {
	// ClaimAffinity atomically claims ownership of a session token for the
	// caller's server. Returns the owning serverID — the caller if the
	// claim succeeded, the prior owner otherwise. The TTL bounds the worst-
	// case time before another server may reclaim if the owner dies.
	ClaimAffinity(ctx context.Context, token protocol.SessionToken, serverID string, ttl time.Duration) (string, error)

	// LookupOwner returns the serverID that currently owns the session.
	// Returns ErrRegistryNotFound when the token has no entry.
	LookupOwner(ctx context.Context, token protocol.SessionToken) (string, error)

	// SaveSession persists session metadata. Idempotent — the same call
	// twice with the same arguments is a no-op.
	SaveSession(ctx context.Context, token protocol.SessionToken, meta SessionMeta) error

	// DeleteSession releases the affinity claim and removes metadata.
	// A no-op if the token has no entry.
	DeleteSession(ctx context.Context, token protocol.SessionToken) error

	// SubscribeEvents returns a channel of session-lifecycle events. The
	// channel closes when the passed ctx is cancelled. Multiple subscribers
	// receive each event independently (fan-out).
	SubscribeEvents(ctx context.Context) (<-chan SessionEvent, error)

	// PublishEvent broadcasts a session-lifecycle event to subscribers.
	// Returns no error when there are no subscribers.
	PublishEvent(ctx context.Context, evt SessionEvent) error

	// Ping reports whether the registry's backing store is reachable. It returns
	// nil when healthy and an error otherwise; the readiness probe uses it to
	// drain a pod that has lost its distributed store.
	// The in-process adapter has no external dependency and always returns nil.
	Ping(ctx context.Context) error
}
