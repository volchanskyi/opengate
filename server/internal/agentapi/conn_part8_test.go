package agentapi

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
	"time"
)

func TestAgentConn_HandleSessionAcceptReject(t *testing.T) {
	t.Run("session accept", func(t *testing.T) {
		ac, buf := newTestAgentConn(t, uuid.New(), nil)
		writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
			Type:  protocol.MsgSessionAccept,
			Token: protocol.GenerateSessionToken(),
		})
		require.NoError(t, ac.handleControl(context.Background()))
	})

	t.Run("session reject", func(t *testing.T) {
		ac, buf := newTestAgentConn(t, uuid.New(), nil)
		writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
			Type:   protocol.MsgSessionReject,
			Token:  protocol.GenerateSessionToken(),
			Reason: "not supported",
		})
		require.NoError(t, ac.handleControl(context.Background()))
	})
}

func TestAgentConn_HandlePingFrame(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	require.NoError(t, ac.codec.WriteFrame(buf, protocol.FramePing, nil))

	require.NoError(t, ac.handleControl(context.Background()))
}

func TestAgentConn_HandleUnknownMessage(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentUpdate,
	})

	require.NoError(t, ac.handleControl(context.Background()))
}

func TestAgentConn_KnownMessageDispatchesAfterUnknownMessage(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	d := testutil.SeedDevice(t, ctx, store, group.ID)

	ac, buf := newTestAgentConn(t, d.ID, store)
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.ControlMessageType("FutureTelemetryWindow"),
	})
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: time.Now().Unix(),
	})

	require.NoError(t, ac.handleControl(ctx))
	require.NoError(t, ac.handleControl(ctx))

	updated, err := testutil.NewTestDevices(t, store).Get(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, device.StatusOnline, updated.Status)
}
