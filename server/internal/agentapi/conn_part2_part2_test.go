package agentapi

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
	"time"
)

func TestAgentConn_HandleHeartbeat(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)
	d := testutil.SeedDevice(t, ctx, store, site.ID)

	codec := &protocol.Codec{}

	ts := time.Now().Unix()
	msg := &protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: ts,
	}
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)

	var frameBuf bytes.Buffer
	err = codec.WriteFrame(&frameBuf, protocol.FrameControl, payload)
	require.NoError(t, err)

	ac := &AgentConn{
		DeviceID: d.ID,
		SiteID:   site.ID,
		stream:   &frameBuf,
		codec:    codec,
		devices:  testutil.NewTestDevices(t, store),
		hardware: testutil.NewTestHardware(t, store),
		logger:   testLogger(),
	}

	err = ac.handleControl(ctx)
	require.NoError(t, err)

	// Verify device status is online
	updated, err := testutil.NewTestDevices(t, store).Get(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, db.StatusOnline, updated.Status)
}
