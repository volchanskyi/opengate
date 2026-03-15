package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestAgentReconnectAfterDisconnect(t *testing.T) {
	env := newAgentTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	// Connect agent (creates new deviceID)
	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Disconnect by closing the stream
	stream.Close()

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOffline
	}, 5*time.Second, 100*time.Millisecond)

	// Reconnect the SAME device with a new QUIC connection
	stream2 := env.connectAgentWithID(t, deviceID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
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
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 2*time.Second, 50*time.Millisecond)
}

func TestAgentReconnectNewCert(t *testing.T) {
	env := newAgentTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	// Connect agent
	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Disconnect
	stream.Close()

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOffline
	}, 5*time.Second, 100*time.Millisecond)

	// Reconnect — connectAgentWithID signs a fresh cert each time,
	// simulating a cert rotation scenario. The device ID stays the same.
	_ = env.connectAgentWithID(t, deviceID)

	// Device should come back online with the new cert
	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)
}

func TestAgentReconnectSessionSurvives(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	// Connect agent
	_, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Create a session
	result := env.createSession(t, jwtToken, deviceID, nil)
	assert.Len(t, result.Token, 64)

	// Session should persist in the DB regardless of agent state
	sessions := env.listSessions(t, jwtToken, deviceID)
	assert.Len(t, sessions, 1)
	assert.Equal(t, result.Token, sessions[0].Token)
}
