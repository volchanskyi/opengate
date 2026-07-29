package amt_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
)

// stubOperator is a minimal Operator test double.
type stubOperator struct {
	gotPowerID    uuid.UUID
	gotPowerState int
	powerErr      error
}

func (s *stubOperator) PowerAction(_ context.Context, id uuid.UUID, state int) error {
	s.gotPowerID = id
	s.gotPowerState = state
	return s.powerErr
}
func (s *stubOperator) QueryDeviceInfo(context.Context, uuid.UUID) (*wsman.DeviceInfo, error) {
	return nil, nil
}
func (s *stubOperator) ConnectedDeviceCount() int { return 0 }

// The amt module's Handlers struct is the per-domain use-case layer. Discovery
// is served by the device read — a device carries its AMT property in its own
// payload — so the api package's transport handler delegates only PowerAction.

func TestHandlers_PowerAction_DelegatesAllArgs(t *testing.T) {
	id := uuid.New()
	op := &stubOperator{}
	h := amt.NewHandlers(op)

	err := h.PowerAction(context.Background(), id, 8 /* PowerOn */)

	require.NoError(t, err)
	require.Equal(t, id, op.gotPowerID)
	require.Equal(t, 8, op.gotPowerState)
}

func TestHandlers_PowerAction_PassesThroughNotConnectedError(t *testing.T) {
	op := &stubOperator{powerErr: amt.ErrDeviceNotConnected}
	h := amt.NewHandlers(op)

	err := h.PowerAction(context.Background(), uuid.New(), 5)

	require.ErrorIs(t, err, amt.ErrDeviceNotConnected)
}
