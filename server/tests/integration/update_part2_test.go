package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/updater"
	"testing"
	"time"
)

// TestUpdatePublishAndPush verifies the full update flow:
// admin publishes manifest → pushes update → connected agent receives AgentUpdate
// control message on QUIC stream → agent sends AgentUpdateAck → DB records status.
func TestUpdatePublishAndPush(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	admin, _ := testutil.SeedAdminUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, admin.ID)

	adminJWT, err := env.jwt.GenerateToken(admin.ID, admin.Email, admin.IsAdmin)
	require.NoError(t, err)

	// Connect an agent that reports as linux/amd64 version 0.13.0
	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// 1. Publish manifest for linux/amd64 v0.14.0
	manifest := publishManifest(t, env, adminJWT, "0.14.0", "linux", "amd64")
	assert.Equal(t, "0.14.0", manifest.Version)

	// 2. Push update to all eligible agents
	result := pushUpdate(t, env, adminJWT, "0.14.0", "linux", "amd64")
	assert.Equal(t, 1, result.PushedCount, "one agent should receive the update")

	// 3. Agent should receive AgentUpdate on QUIC control stream
	codec := &protocol.Codec{}
	frameType, payload, err := codec.ReadFrame(stream)
	require.NoError(t, err)
	assert.Equal(t, protocol.FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgAgentUpdate, msg.Type)
	assert.Equal(t, "0.14.0", msg.Version)
	assert.Contains(t, msg.URL, "agent-0.14.0")
	assert.NotEmpty(t, msg.SHA256)
	assert.NotEmpty(t, msg.Signature)

	// 4. Agent sends AgentUpdateAck back
	success := true
	ackMsg := &protocol.ControlMessage{
		Type:    protocol.MsgAgentUpdateAck,
		Version: "0.14.0",
		Success: &success,
	}
	ackPayload, err := codec.EncodeControl(ackMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, ackPayload))

	// 5. Verify DB recorded the pending update (created synchronously in the push handler)
	updates, err := env.deviceUpdates.ListByVersion(defaultTenantContext(), "0.14.0")
	require.NoError(t, err)
	require.NotEmpty(t, updates, "push handler should have created a device_update record")

	found := false
	for _, u := range updates {
		if u.DeviceID == deviceID {
			// Status may be "pending" or "success" depending on whether the
			// server processed the AgentUpdateAck before this query runs.
			assert.Contains(t, []updater.Status{updater.StatusPending, updater.StatusSuccess}, u.Status)
			found = true
		}
	}
	assert.True(t, found, "device update record should exist for device %s", deviceID)
}
