package relay

import (
	"context"
	"sync"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// InProcessRegistry is the in-memory SessionRegistry for single-server deployments.
// It keeps only the token to owning serverID mapping and discards the rest of the metadata.
type InProcessRegistry struct {
	mu      sync.Mutex
	entries map[protocol.SessionToken]string
}

// NewInProcessRegistry returns a SessionRegistry backed by in-memory state.
func NewInProcessRegistry() *InProcessRegistry {
	return &InProcessRegistry{
		entries: make(map[protocol.SessionToken]string),
	}
}

// SaveSession creates an entry owned by meta.ServerID and leaves an existing entry untouched.
func (r *InProcessRegistry) SaveSession(_ context.Context, token protocol.SessionToken, meta SessionMeta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[token]; !ok {
		r.entries[token] = meta.ServerID
	}
	return nil
}

// DeleteSession removes the token's entry.
func (r *InProcessRegistry) DeleteSession(_ context.Context, token protocol.SessionToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, token)
	return nil
}

// Ping always returns nil because the state is local memory.
func (r *InProcessRegistry) Ping(context.Context) error {
	return nil
}
