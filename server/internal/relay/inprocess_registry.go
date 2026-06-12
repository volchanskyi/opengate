package relay

import (
	"context"
	"sync"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// InProcessRegistry is the SessionRegistry adapter for single-server
// deployments — currently the only adapter. State lives in-memory. The port seam
// is retained so a future distributed adapter can be added without touching
// consumers.
//
// The metadata passed to SaveSession is not persisted: in a single-server
// deployment no peer exists to query it, so only the token → owning serverID
// mapping is kept. A future distributed adapter would persist the full metadata.
type InProcessRegistry struct {
	mu      sync.Mutex
	entries map[protocol.SessionToken]string // token → owning serverID
}

// NewInProcessRegistry returns a SessionRegistry backed by in-memory state.
func NewInProcessRegistry() *InProcessRegistry {
	return &InProcessRegistry{
		entries: make(map[protocol.SessionToken]string),
	}
}

// SaveSession implements SessionRegistry. Creates an entry owned by
// meta.ServerID if none exists; otherwise leaves the existing entry untouched.
func (r *InProcessRegistry) SaveSession(_ context.Context, token protocol.SessionToken, meta SessionMeta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[token]; !ok {
		r.entries[token] = meta.ServerID
	}
	return nil
}

// DeleteSession implements SessionRegistry.
func (r *InProcessRegistry) DeleteSession(_ context.Context, token protocol.SessionToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, token)
	return nil
}

// Ping reports the in-process registry as always reachable — it keeps state in
// local memory with no external dependency to lose.
func (r *InProcessRegistry) Ping(context.Context) error {
	return nil
}
