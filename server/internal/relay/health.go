package relay

import "context"

// PingRegistry reports whether the relay's SessionRegistry is ready. The
// in-process adapter has no external dependency and always returns nil.
func (r *Relay) PingRegistry(ctx context.Context) error {
	return r.registry.Ping(ctx)
}
