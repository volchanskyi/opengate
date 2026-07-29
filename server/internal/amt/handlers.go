package amt

import (
	"context"

	"github.com/google/uuid"
)

// Handlers exposes the amt module's use cases to transport-layer callers.
//
// The api package's transport handlers translate HTTP requests and responses to
// method calls on this struct. Discovery is served by the device read — a device
// carries its AMT property in its own payload — so the only use case left here
// is live device interaction through the Operator port, passed in at
// construction so tests can substitute a fake.
type Handlers struct {
	operator Operator
}

// NewHandlers wires a Handlers struct against the operator port.
func NewHandlers(op Operator) *Handlers {
	return &Handlers{operator: op}
}

// PowerAction sends a power command (PowerOn / PowerCycle / SoftOff /
// HardReset) to a connected AMT device, surfacing ErrDeviceNotConnected
// when the device has no active CIRA tunnel.
func (h *Handlers) PowerAction(ctx context.Context, id uuid.UUID, state int) error {
	return h.operator.PowerAction(ctx, id, state)
}
