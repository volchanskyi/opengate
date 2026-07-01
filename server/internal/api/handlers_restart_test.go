package api

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"log/slog"
	"os"
	"testing"
)

// deviceTestEnv holds common setup for restart and hardware handler tests.
type deviceTestEnv struct {
	store         *db.PostgresStore
	ctx           context.Context
	devices       device.Repository
	hardware      device.HardwareRepository
	deviceLogs    device.LogsRepository
	device        *device.Device
	srv           *Server
	ownerToken    string
	agentStream   *bytes.Buffer
	generateToken func(userID uuid.UUID, email string, isAdmin bool) (string, error)
}

// setupDeviceTest creates a user, group, device, and test server. When online
// is true an AgentConn backed by agentStream is registered.
func setupDeviceTest(t *testing.T, online bool) *deviceTestEnv {
	t.Helper()

	var agentStream bytes.Buffer
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(t.Context(), true)

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	device := testutil.SeedDevice(t, ctx, store, group.ID)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	lookup := &stubAgentGetter{}
	if online {
		ac := agentapi.NewAgentConn(agentapi.AgentConnConfig{DeviceID: device.ID, GroupID: group.ID, Stream: &agentStream, Devices: testutil.NewTestDevices(t, store), Hardware: testutil.NewTestHardware(t, store), DeviceLogs: testutil.NewTestLogs(t, store), DeviceUpdates: testutil.NewTestDeviceUpdates(t, store), Logger: logger})
		ac.Capabilities = []protocol.AgentCapability{protocol.CapHardwareInventory, protocol.CapDeviceLogs}
		lookup = &stubAgentGetter{
			agents: map[protocol.DeviceID]*agentapi.AgentConn{device.ID: ac},
		}
	}

	srv, cfg := newTestServerWithStoreAndAgents(t, store, lookup, relay.NewRelay(slog.Default()))

	token, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin, user.OrgID)
	require.NoError(t, err)

	return &deviceTestEnv{
		store:       store,
		ctx:         ctx,
		devices:     testutil.NewTestDevices(t, store),
		hardware:    testutil.NewTestHardware(t, store),
		deviceLogs:  testutil.NewTestLogs(t, store),
		device:      device,
		srv:         srv,
		ownerToken:  token,
		agentStream: &agentStream,
		generateToken: func(userID uuid.UUID, email string, isAdmin bool) (string, error) {
			return cfg.GenerateToken(userID, email, isAdmin)
		},
	}
}
