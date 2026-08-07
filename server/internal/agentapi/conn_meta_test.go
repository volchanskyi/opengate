package agentapi

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// TestAgentConn_MetaSnapshot pins that Meta() returns the registration-reported
// OS/Arch/AgentVersion plus the immutable DeviceID.
func TestAgentConn_MetaSnapshot(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)
	deviceID := uuid.New()

	ac := newMetaTestConn(t, store, deviceID, site.ID)
	require := assert.New(t)
	require.NoError(ac.handleRegister(ctx, registerMsg()))

	got := ac.Meta()
	require.Equal(deviceID, got.DeviceID)
	require.Equal("linux", got.OS)
	require.Equal("amd64", got.Arch)
	require.Equal("1.2.3", got.AgentVersion)
}

// TestAgentConn_MetaRace runs registration (the read-loop write path) concurrently
// with Meta() and requireCapability reads (the HTTP read path). Under `go test
// -race` it proves OS/Arch/AgentVersion/Capabilities are guarded — without the
// guard the detector flags handleRegister's writes against the concurrent reads.
func TestAgentConn_MetaRace(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)
	deviceID := uuid.New()

	ac := newMetaTestConn(t, store, deviceID, site.ID)
	msg := registerMsg()

	var wg sync.WaitGroup
	// One writer on the read-loop goroutine: repeated registration.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 50 {
			assert.NoError(t, ac.handleRegister(ctx, msg))
		}
	}()
	// Concurrent readers on HTTP goroutines: Meta() + requireCapability.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_ = ac.Meta()
				_ = ac.requireCapability(protocol.CapHardwareInventory)
			}
		}()
	}
	wg.Wait()

	got := ac.Meta()
	assert.Equal(t, "linux", got.OS)
	assert.Equal(t, "amd64", got.Arch)
	assert.Equal(t, "1.2.3", got.AgentVersion)
	assert.NoError(t, ac.requireCapability(protocol.CapDeviceLogs))
}

// TestAgentConn_RegisterRequestsHardwareReport pins the pull that keeps the
// device page's Hardware card current: a device that reboots or reconnects with
// swapped RAM or a new NIC re-reports as it registers, so the stored row is
// refreshed by coming back online rather than by an operator asking.
func TestAgentConn_RegisterRequestsHardwareReport(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)

	ac := newMetaTestConn(t, store, uuid.New(), site.ID)
	buf := ac.stream.(*bytes.Buffer)

	require.NoError(t, ac.handleRegister(ctx, registerMsg()))

	assert.Equal(t, protocol.MsgRequestHardwareReport, readOutboundControl(t, ac, buf).Type)
}

// TestAgentConn_RegisterWithoutHardwareCapabilitySendsNothing keeps registration
// clean for an agent that never advertised the inventory: the unsupported pull
// is skipped rather than written and rejected.
func TestAgentConn_RegisterWithoutHardwareCapabilitySendsNothing(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)

	ac := newMetaTestConn(t, store, uuid.New(), site.ID)
	buf := ac.stream.(*bytes.Buffer)
	msg := registerMsg()
	msg.Capabilities = []protocol.AgentCapability{protocol.CapDeviceLogs}

	require.NoError(t, ac.handleRegister(ctx, msg))

	assert.Zero(t, buf.Len())
}

func newMetaTestConn(t *testing.T, store *db.PostgresStore, deviceID, siteID uuid.UUID) *AgentConn {
	t.Helper()
	return &AgentConn{
		DeviceID: deviceID,
		SiteID:   siteID,
		stream:   &bytes.Buffer{},
		codec:    &protocol.Codec{},
		devices:  testutil.NewTestDevices(t, store),
		hardware: testutil.NewTestHardware(t, store),
		logger:   testLogger(),
	}
}

func registerMsg() *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapHardwareInventory, protocol.CapDeviceLogs},
		Hostname:     "race-host",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "1.2.3",
	}
}
