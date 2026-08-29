package agentapi

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// newTestAgentServer builds an AgentServer wired up with a temp cert manager,
// in-memory store, noop notifier, and a default relay — everything callers need
// to exercise AgentServer methods without QUIC listeners.
func newTestAgentServer(t *testing.T) *AgentServer {
	t.Helper()
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	store := testutil.NewTestStore(t)
	return NewAgentServer(AgentServerConfig{
		Cert:          cm,
		Devices:       testutil.NewTestDevices(t, store),
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Relay:         relay.NewRelay(slog.Default()),
		Notifier:      &notifications.NoopNotifier{},
		QuicHost:      "",
		Logger:        testLogger(),
	})
}

func TestAgentServer_ConnectedAgentCount_ZeroAtStart(t *testing.T) {
	srv := newTestAgentServer(t)
	assert.Equal(t, 0, srv.ConnectedAgentCount())
}

func TestAgentServer_ReconnectRaceCondition(t *testing.T) {
	srv := newTestAgentServer(t)

	deviceID := protocol.DeviceID(uuid.New())
	ctx := context.Background()

	// The first connection registers through the same door a real one uses.
	oldConn := &AgentConn{DeviceID: deviceID}
	srv.registerConn(ctx, oldConn, "host")
	assert.Equal(t, 1, srv.ConnectedAgentCount())

	// The machine dials again before that one has torn down.
	newConn := &AgentConn{DeviceID: deviceID}
	srv.registerConn(ctx, newConn, "host")
	assert.Equal(t, 1, srv.ConnectedAgentCount(),
		"one machine is one machine, however many connections it has open")

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

func TestAgentServer_ListConnectedAgents(t *testing.T) {
	srv := newTestAgentServer(t)

	// Empty at start
	assert.Empty(t, srv.ListConnectedAgents())

	// Add two connections
	d1, d2 := uuid.New(), uuid.New()
	srv.conns.Store(d1, &AgentConn{DeviceID: d1})
	srv.conns.Store(d2, &AgentConn{DeviceID: d2})

	agents := srv.ListConnectedAgents()
	assert.Len(t, agents, 2)
}

func TestAgentServer_ScopeForDeviceUsesStoredTenant(t *testing.T) {
	store := testutil.NewTestStore(t)
	tenantB := uuid.New()
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])
	site := testutil.SeedSite(t, ctxB, store)
	d := testutil.SeedDevice(t, ctxB, store, site.ID)
	srv := AgentServer{
		devices: testutil.NewTestDevices(t, store),
		logger:  testLogger(),
	}

	scoped := srv.scopeForDevice(context.Background(), d.ID, testLogger())

	tenant, ok := dbtx.TenantFromContext(scoped)
	require.True(t, ok)
	assert.Equal(t, tenantB, tenant.TenantID)
	assert.False(t, tenant.IsAdmin)
}

func TestAgentServer_DeregisterAgent(t *testing.T) {
	srv := newTestAgentServer(t)

	t.Run("device not connected", func(t *testing.T) {
		// Should not panic when agent is offline
		srv.DeregisterAgent(context.Background(), uuid.New())
	})

	t.Run("device connected", func(t *testing.T) {
		deviceID := uuid.New()
		var buf bytes.Buffer
		ac := &AgentConn{
			DeviceID: deviceID,
			stream:   &buf,
			codec:    &protocol.Codec{},
			logger:   testLogger(),
		}
		srv.conns.Store(deviceID, ac)

		srv.DeregisterAgent(context.Background(), deviceID)

		// Should be tombstoned
		_, ok := srv.tombstones.Load(deviceID)
		assert.True(t, ok, "device should be tombstoned")
	})
}

func TestAgentServer_StopsOnContextCancel(t *testing.T) {
	srv := newTestAgentServer(t)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	// Give it a moment to start
	cancel()

	err := <-errCh
	// Should return nil or context.Canceled
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}
