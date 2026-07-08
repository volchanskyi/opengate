package agentapi

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/updater"
	"testing"
)

func TestAgentConn_HandleAgentUpdateAck(t *testing.T) {
	store := testutil.NewTestStore(t)
	deviceUpdates := testutil.NewTestDeviceUpdates(t, store)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	d := testutil.SeedDevice(t, ctx, store, group.ID)

	// Create a pending update record
	require.NoError(t, deviceUpdates.Create(ctx, &updater.DeviceUpdate{
		DeviceID: d.ID,
		Version:  "0.5.0",
		Status:   updater.StatusPending,
	}))

	codec := &protocol.Codec{}

	// findUpdate returns the latest DeviceUpdate record for (device, version).
	findUpdate := func(t *testing.T) *updater.DeviceUpdate {
		t.Helper()
		ups, err := deviceUpdates.ListByVersion(ctx, "0.5.0")
		require.NoError(t, err)
		for _, u := range ups {
			if u.DeviceID == d.ID {
				return u
			}
		}
		t.Fatalf("no update record for %s @ 0.5.0", d.ID)
		return nil
	}

	t.Run("success ack persists Success status", func(t *testing.T) {
		success := true
		msg := &protocol.ControlMessage{
			Type:    protocol.MsgAgentUpdateAck,
			Version: "0.5.0",
			Success: &success,
		}
		payload, err := codec.EncodeControl(msg)
		require.NoError(t, err)

		var frameBuf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&frameBuf, protocol.FrameControl, payload))

		ac := &AgentConn{
			DeviceID:      d.ID,
			GroupID:       group.ID,
			stream:        &frameBuf,
			codec:         codec,
			devices:       testutil.NewTestDevices(t, store),
			hardware:      testutil.NewTestHardware(t, store),
			deviceUpdates: deviceUpdates,
			logger:        testLogger(),
		}
		require.NoError(t, ac.handleControl(ctx))

		// Pin the success path: status must be Success. Without this assertion
		// CONDITIONALS_NEGATION on `msg.Success != nil` survives because
		// handleControl returns nil regardless of the persisted outcome.
		got := findUpdate(t)
		assert.Equal(t, updater.StatusSuccess, got.Status,
			"success=true ack must persist updater.StatusSuccess")
	})

	t.Run("failure ack persists Failed status", func(t *testing.T) {
		success := false
		msg := &protocol.ControlMessage{
			Type:     protocol.MsgAgentUpdateAck,
			Version:  "0.5.0",
			Success:  &success,
			AckError: "checksum mismatch",
		}
		payload, err := codec.EncodeControl(msg)
		require.NoError(t, err)

		var frameBuf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&frameBuf, protocol.FrameControl, payload))

		ac := &AgentConn{
			DeviceID:      d.ID,
			GroupID:       group.ID,
			stream:        &frameBuf,
			codec:         codec,
			devices:       testutil.NewTestDevices(t, store),
			hardware:      testutil.NewTestHardware(t, store),
			deviceUpdates: deviceUpdates,
			logger:        testLogger(),
		}
		require.NoError(t, ac.handleControl(ctx))

		got := findUpdate(t)
		assert.Equal(t, updater.StatusFailed, got.Status,
			"success=false ack must persist updater.StatusFailed")
		assert.Equal(t, "checksum mismatch", got.Error)
	})
}
