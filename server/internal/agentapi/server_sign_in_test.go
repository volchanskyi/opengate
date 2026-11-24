package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// signInCloseWait bounds how long a turned-away machine waits for the server's close.
const signInCloseWait = 10 * time.Second

func TestAMachineWhoseSignInFailsIsTurnedAwayAndHeldNowhere(t *testing.T) {
	env := newAcceptEnvOver(t, nil, func(d device.Repository) device.Repository {
		return refusingDevices{Repository: d}
	})
	deviceID := uuid.New()

	conn, stream := env.connect(t, deviceID)
	register(t, stream, "dal-ws-012")

	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseBye), closedWithin(t, conn, signInCloseWait),
		"the machine is told to dial again")
	assert.Zero(t, env.srv.ConnectedAgentCount())
	assert.Nil(t, env.srv.GetAgent(protocol.DeviceID(deviceID)))
}

func TestAMachineOfAnotherTenantSignsInToItsOwnTenant(t *testing.T) {
	env := newAcceptEnv(t)
	northwind := uuid.New()
	ctx := dbtx.WithTenant(context.Background(), northwind, false)
	testutil.EnsureTenant(t, ctx, env.store, northwind, "northwind-"+northwind.String()[:8])
	site := testutil.SeedSite(t, ctx, env.store)
	machine := testutil.SeedDevice(t, ctx, env.store, site.ID)

	_, stream := env.connect(t, machine.ID)
	register(t, stream, "nw-ws-001")

	awaitSignedIn(t, stream)
	ac := env.srv.GetAgent(protocol.DeviceID(machine.ID))
	require.NotNil(t, ac)
	assert.Equal(t, northwind, ac.TenantID)
	assert.Equal(t, site.ID, ac.SiteID)
	stored, err := env.devices.Get(ctx, machine.ID)
	require.NoError(t, err)
	assert.Equal(t, "nw-ws-001", stored.Hostname, "the sign-in wrote the tenant's own row")
	assert.Equal(t, db.StatusOnline, stored.Status)
}

func TestAMachineWhoseFirstMessageIsNotItsSignInIsClosed(t *testing.T) {
	env := newAcceptEnv(t)
	conn, stream := env.connect(t, uuid.New())

	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(&protocol.ControlMessage{Type: protocol.MsgAgentHeartbeat})
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, payload))

	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseBye), closedWithin(t, conn, signInCloseWait))
	assert.Zero(t, env.srv.ConnectedAgentCount())
}

func TestAPingBeforeTheSignInIsAnswered(t *testing.T) {
	env := newAcceptEnv(t)
	_, stream := env.connect(t, uuid.New())

	codec := &protocol.Codec{}
	require.NoError(t, codec.WriteFrame(stream, protocol.FramePing, nil))
	frameType, _, err := codec.ReadFrame(stream)
	require.NoError(t, err)
	assert.Equal(t, protocol.FramePong, frameType)

	register(t, stream, "dal-ws-013")
	waitForCount(t, env.srv, 1)
}
