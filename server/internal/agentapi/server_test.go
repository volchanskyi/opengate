package agentapi

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestAgentServer_ConnectedAgentCount_ZeroAtStart(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	store := testutil.NewTestStore(t)
	r := relay.NewRelay(slog.Default())

	srv := NewAgentServer(cm, store, r, &notifications.NoopNotifier{}, "", testLogger())
	assert.Equal(t, 0, srv.ConnectedAgentCount())
}

func TestAgentServer_ReconnectRaceCondition(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	store := testutil.NewTestStore(t)
	r := relay.NewRelay(slog.Default())
	srv := NewAgentServer(cm, store, r, &notifications.NoopNotifier{}, "", testLogger())

	deviceID := protocol.DeviceID(uuid.New())

	// Simulate first connection registered in map.
	oldConn := &AgentConn{DeviceID: deviceID}
	srv.conns.Store(deviceID, oldConn)
	srv.count.Add(1)

	// Simulate rapid reconnect: new connection replaces old in map.
	newConn := &AgentConn{DeviceID: deviceID}
	srv.conns.Store(deviceID, newConn)
	srv.count.Add(1) // count is now 2 (both registered)

	// Old connection's defer runs CompareAndDelete with oldConn pointer.
	// It should NOT delete the new connection.
	deleted := srv.conns.CompareAndDelete(deviceID, oldConn)
	assert.False(t, deleted, "old connection should NOT delete newer entry")

	// New connection should still be retrievable.
	got := srv.GetAgent(deviceID)
	require.NotNil(t, got)
	assert.Equal(t, newConn, got, "new connection must survive old defer")

	// Now simulate new connection disconnecting normally.
	deleted = srv.conns.CompareAndDelete(deviceID, newConn)
	assert.True(t, deleted, "current connection should be deleted")
	assert.Nil(t, srv.GetAgent(deviceID))
}

func TestAgentServer_StopsOnContextCancel(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	store := testutil.NewTestStore(t)
	r := relay.NewRelay(slog.Default())

	srv := NewAgentServer(cm, store, r, &notifications.NoopNotifier{}, "", testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	// Give it a moment to start
	cancel()

	err = <-errCh
	// Should return nil or context.Canceled
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}
