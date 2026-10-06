package audit

import "context"

// Handlers exposes the audit module's use cases to transport-layer callers.
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
