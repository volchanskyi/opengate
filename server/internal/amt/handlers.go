package amt

import (
	"context"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// Handlers exposes the amt module's use cases to transport-layer callers.
//
// Per ADR-020 §9 + modular-monolith plan §4.1, the api package's transport
// handlers translate HTTP requests and responses to method calls on this
// struct. The Repository handles persistence (List, Get); the Operator
// handles live device interaction (PowerAction). Both ports are passed in
// at construction so tests can substitute fakes.
type Handlers struct {
	repo     Repository
	operator Operator
}

// NewHandlers wires a Handlers struct against the persistence + operator ports.
func NewHandlers(repo Repository, op Operator) *Handlers {
	return &Handlers{repo: repo, operator: op}
}

// ListDevices returns every known AMT device.
func (h *Handlers) ListDevices(ctx context.Context) ([]*db.AMTDevice, error) {
	return h.repo.List(ctx)
}

// GetDevice returns a single AMT device by UUID, or ErrAMTDeviceNotFound.
func (h *Handlers) GetDevice(ctx context.Context, id uuid.UUID) (*db.AMTDevice, error) {
	return h.repo.Get(ctx, id)
}

// PowerAction sends a power command (PowerOn / PowerCycle / SoftOff /
// HardReset) to a connected AMT device, surfacing ErrDeviceNotConnected
// when the device has no active CIRA tunnel.
func (h *Handlers) PowerAction(ctx context.Context, id uuid.UUID, state int) error {
	return h.operator.PowerAction(ctx, id, state)
}
