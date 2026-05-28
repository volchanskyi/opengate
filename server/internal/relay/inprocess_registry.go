package relay

import (
	"context"
	"sync"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// InProcessRegistry is the SessionRegistry adapter for single-server
// deployments. State lives in-memory; affinity is trivial (the calling
// server always wins its first claim). Phase 13b replaces this adapter
// with RedisRegistry without touching consumers.
//
// TTL is ignored by this adapter — there is no crash-recovery scenario
// for a single server. RedisRegistry honors TTL.
type InProcessRegistry struct {
	mu      sync.Mutex
	entries map[protocol.SessionToken]*registryEntry

	subMu       sync.RWMutex
	subscribers []chan SessionEvent
}

type registryEntry struct {
	serverID string
	meta     SessionMeta
	hasMeta  bool
}

// NewInProcessRegistry returns a SessionRegistry backed by in-memory state.
func NewInProcessRegistry() *InProcessRegistry {
	return &InProcessRegistry{
		entries: make(map[protocol.SessionToken]*registryEntry),
	}
}

// ClaimAffinity implements SessionRegistry.
func (r *InProcessRegistry) ClaimAffinity(_ context.Context, token protocol.SessionToken, serverID string, _ time.Duration) (string, error) {
	if serverID == "" {
		return "", ErrInvalidArgument
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[token]; ok {
		return e.serverID, nil
	}
	r.entries[token] = &registryEntry{serverID: serverID}
	return serverID, nil
}

// LookupOwner implements SessionRegistry.
func (r *InProcessRegistry) LookupOwner(_ context.Context, token protocol.SessionToken) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[token]
	if !ok {
		return "", ErrRegistryNotFound
	}
	return e.serverID, nil
}

// SaveSession implements SessionRegistry. Creates an entry if none exists.
func (r *InProcessRegistry) SaveSession(_ context.Context, token protocol.SessionToken, meta SessionMeta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[token]
	if !ok {
		e = &registryEntry{serverID: meta.ServerID}
		r.entries[token] = e
	}
	e.meta = meta
	e.hasMeta = true
	return nil
}

// DeleteSession implements SessionRegistry.
func (r *InProcessRegistry) DeleteSession(_ context.Context, token protocol.SessionToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, token)
	return nil
}

// SubscribeEvents implements SessionRegistry. The returned channel is closed
// when ctx is cancelled.
func (r *InProcessRegistry) SubscribeEvents(ctx context.Context) (<-chan SessionEvent, error) {
	ch := make(chan SessionEvent, 16)

	r.subMu.Lock()
	r.subscribers = append(r.subscribers, ch)
	r.subMu.Unlock()

	go func() {
		<-ctx.Done()
		r.removeSubscriber(ch)
		close(ch)
	}()

	return ch, nil
}

// PublishEvent implements SessionRegistry.
func (r *InProcessRegistry) PublishEvent(_ context.Context, evt SessionEvent) error {
	r.subMu.RLock()
	subs := make([]chan SessionEvent, len(r.subscribers))
	copy(subs, r.subscribers)
	r.subMu.RUnlock()

	for _, ch := range subs {
		// Non-blocking send to a buffered channel — a slow subscriber drops
		// events rather than backpressuring the publisher. RedisRegistry will
		// have similar behavior via Pub/Sub semantics.
		select {
		case ch <- evt:
		default:
		}
	}
	return nil
}

func (r *InProcessRegistry) removeSubscriber(target chan SessionEvent) {
	r.subMu.Lock()
	defer r.subMu.Unlock()
	for i, ch := range r.subscribers {
		if ch == target {
			r.subscribers = append(r.subscribers[:i], r.subscribers[i+1:]...)
			return
		}
	}
}
