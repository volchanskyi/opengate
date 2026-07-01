package integration

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
	"time"
)

func TestAgentReconnectAfterDisconnect(t *testing.T) {
	t.Parallel()
	env := newAgentTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	// Connect agent (creates new deviceID)
	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Disconnect by closing the stream
	stream.Close()

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOffline
	}, 5*time.Second, 100*time.Millisecond)

	// Reconnect the SAME device with a new QUIC connection
	stream2 := env.connectAgentWithID(t, deviceID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Verify agent is functional — send heartbeat
	codec := &protocol.Codec{}
	hbMsg := &protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: time.Now().Unix(),
	}
	payload, err := codec.EncodeControl(hbMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream2, protocol.FrameControl, payload))

	// Confirm heartbeat processed
	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 2*time.Second, 50*time.Millisecond)
}
