package amt_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// stubRepo is a minimal Repository test double recording inputs.
type stubRepo struct {
	gotGetID uuid.UUID
	listOut  []*db.AMTDevice
	getOut   *db.AMTDevice
	err      error
}

func (s *stubRepo) Upsert(context.Context, *db.AMTDevice) error { return nil }
func (s *stubRepo) Get(_ context.Context, id uuid.UUID) (*db.AMTDevice, error) {
	s.gotGetID = id
	return s.getOut, s.err
}
func (s *stubRepo) List(context.Context) ([]*db.AMTDevice, error) { return s.listOut, s.err }
func (s *stubRepo) SetStatus(context.Context, uuid.UUID, db.DeviceStatus) error {
	return nil
}

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

// The amt module's Handlers struct is the
// per-domain use-case layer. The api package's transport handler delegates
// to ListDevices / GetDevice / PowerAction.

func TestHandlers_ListDevices_DelegatesToRepository(t *testing.T) {
	want := []*db.AMTDevice{
		{UUID: uuid.New(), Hostname: "a"},
		{UUID: uuid.New(), Hostname: "b"},
	}
	repo := &stubRepo{listOut: want}
	h := amt.NewHandlers(repo, &stubOperator{})

	got, err := h.ListDevices(context.Background())

	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestHandlers_ListDevices_PassesThroughError(t *testing.T) {
	repo := &stubRepo{err: errors.New("boom")}
	h := amt.NewHandlers(repo, &stubOperator{})

	_, err := h.ListDevices(context.Background())

	require.EqualError(t, err, "boom")
}

func TestHandlers_GetDevice_DelegatesIDAndResult(t *testing.T) {
	id := uuid.New()
	want := &db.AMTDevice{UUID: id, Hostname: "x"}
	repo := &stubRepo{getOut: want}
	h := amt.NewHandlers(repo, &stubOperator{})

	got, err := h.GetDevice(context.Background(), id)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, id, repo.gotGetID, "Handlers.GetDevice must pass the UUID through unchanged")
}

func TestHandlers_GetDevice_PassesThroughNotFoundError(t *testing.T) {
	repo := &stubRepo{err: amt.ErrAMTDeviceNotFound}
	h := amt.NewHandlers(repo, &stubOperator{})

	_, err := h.GetDevice(context.Background(), uuid.New())

	require.ErrorIs(t, err, amt.ErrAMTDeviceNotFound)
}

func TestHandlers_PowerAction_DelegatesAllArgs(t *testing.T) {
	id := uuid.New()
	op := &stubOperator{}
	h := amt.NewHandlers(&stubRepo{}, op)

	err := h.PowerAction(context.Background(), id, 8 /* PowerOn */)

	require.NoError(t, err)
	require.Equal(t, id, op.gotPowerID)
	require.Equal(t, 8, op.gotPowerState)
}

func TestHandlers_PowerAction_PassesThroughNotConnectedError(t *testing.T) {
	op := &stubOperator{powerErr: amt.ErrDeviceNotConnected}
	h := amt.NewHandlers(&stubRepo{}, op)

	err := h.PowerAction(context.Background(), uuid.New(), 5)

	require.ErrorIs(t, err, amt.ErrDeviceNotConnected)
}
