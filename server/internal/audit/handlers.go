package audit

import "context"

// Handlers exposes the audit module's use cases to transport-layer callers.
//
// Per ADR-020 §9 + modular-monolith plan §4.1, the api package's transport
// handlers translate HTTP requests and responses to method calls on this
// struct; the use-case logic lives here. For audit specifically the layer
// is a thin delegation to the Repository — there is no permission check
// or cross-aggregate join at the use-case level (those live in the
// transport handler). The pattern is established so future audit use cases
// (bulk export, retention sweeps, etc.) have a natural home.
type Handlers struct {
	repo Repository
}

// NewHandlers wires a Handlers struct against the persistence port.
func NewHandlers(repo Repository) *Handlers {
	return &Handlers{repo: repo}
}

// ListEvents returns audit events matching q.
func (h *Handlers) ListEvents(ctx context.Context, q Query) ([]*Event, error) {
	return h.repo.Query(ctx, q)
}
