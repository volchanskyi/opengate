package amt

import (
	"context"

	"github.com/google/uuid"
)

// Handlers exposes the amt module's live device use cases through the Operator port.
type Handlers struct {
	operator Operator
}

// NewHandlers wires a Handlers struct against the operator port.
func NewHandlers(op Operator) *Handlers {
	return &Handlers{operator: op}
}

// PowerAction sends a power command to a connected AMT device and returns
// ErrDeviceNotConnected when the device has no active CIRA tunnel.
func (h *Handlers) PowerAction(ctx context.Context, id uuid.UUID, state int) error {
	return h.operator.PowerAction(ctx, id, state)
}
