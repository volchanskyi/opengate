package agentapi

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"log/slog"
	"math"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newTestAgentConn creates an AgentConn backed by an in-memory buffer for testing.
// Returns the conn and the buffer so callers can read back what was written.
// Pass store=nil for tests that do not touch the device/hardware/logs repos.
func newTestAgentConn(t *testing.T, deviceID uuid.UUID, store *db.PostgresStore) (*AgentConn, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ac := &AgentConn{
		DeviceID: deviceID,
		stream:   &buf,
		codec:    &protocol.Codec{},
		logger:   testLogger(),
	}
	if store != nil {
		ac.devices = testutil.NewTestDevices(t, store)
		ac.hardware = testutil.NewTestHardware(t, store)
	}
	return ac, &buf
}

// writeControlMsg encodes a control message and writes it as a framed payload into buf.
func writeControlMsg(t *testing.T, codec *protocol.Codec, buf *bytes.Buffer, msg *protocol.ControlMessage) {
	t.Helper()
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(buf, protocol.FrameControl, payload))
}

// TestClampNonNegativeUint32_Boundaries pins behavior at the [0, MaxUint32]
// edges so CONDITIONALS_BOUNDARY mutants on conn.go:123 (`v <= 0`) and
// conn.go:126 (`uint64(v) > math.MaxUint32`) cannot survive.
func TestClampNonNegativeUint32_Boundaries(t *testing.T) {
	assert.Equal(t, uint32(0), clampNonNegativeUint32(-1))
	assert.Equal(t, uint32(0), clampNonNegativeUint32(0))
	assert.Equal(t, uint32(1), clampNonNegativeUint32(1))
	assert.Equal(t, uint32(math.MaxUint32), clampNonNegativeUint32(int(math.MaxUint32)))
	// Above MaxUint32 must clamp.
	if math.MaxInt > math.MaxUint32 {
		assert.Equal(t, uint32(math.MaxUint32), clampNonNegativeUint32(int(math.MaxUint32)+1))
	}
}

// TestClampInt64_Boundaries pins behavior at math.MaxInt64 so the
// CONDITIONALS_BOUNDARY mutant on conn.go:134 (`v > math.MaxInt64`)
// cannot survive.
func TestClampInt64_Boundaries(t *testing.T) {
	assert.Equal(t, int64(0), clampInt64(0))
	assert.Equal(t, int64(1), clampInt64(1))
	assert.Equal(t, int64(math.MaxInt64), clampInt64(math.MaxInt64))
	// One past MaxInt64 must clamp, not wrap.
	assert.Equal(t, int64(math.MaxInt64), clampInt64(uint64(math.MaxInt64)+1))
}

func TestNewAgentConn(t *testing.T) {
	store := testutil.NewTestStore(t)
	deviceID := uuid.New()
	siteID := uuid.New()
	var buf bytes.Buffer
	logger := testLogger()

	ac := NewAgentConn(AgentConnConfig{
		DeviceID:      deviceID,
		SiteID:        siteID,
		Stream:        &buf,
		Devices:       testutil.NewTestDevices(t, store),
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Logger:        logger,
	})
	assert.Equal(t, deviceID, ac.DeviceID)
	assert.Equal(t, siteID, ac.SiteID)
	assert.NotNil(t, ac.codec)
	assert.NotNil(t, ac.stream)
}

// TestHandleHardwareReportStoresAMTPresence covers the agent-sourced half of the
// AMT link: the join key and the Management Engine reading must reach the
// hardware row, and a malformed key must not.
func TestHandleHardwareReportStoresAMTPresence(t *testing.T) {
	t.Parallel()
	systemUUID := uuid.New()
	available := true

	tests := []struct {
		name     string
		reported string
		wantKey  *uuid.UUID
	}{
		{"well-formed system uuid is stored", systemUUID.String(), &systemUUID},
		{"empty system uuid preserves the stored key", "", nil},
		{"malformed system uuid is rejected", "not-a-uuid", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hw := &recordingHardware{}
			conn := &AgentConn{DeviceID: uuid.New(), hardware: hw, logger: testLogger()}

			require.NoError(t, conn.handleHardwareReport(context.Background(), &protocol.ControlMessage{
				Type:         protocol.MsgHardwareReport,
				CPUModel:     "Intel Core i7-12700K",
				SystemUUID:   tt.reported,
				AMTAvailable: &available,
				AMTVersion:   "16.1.30.2260",
			}))

			require.NotNil(t, hw.last)
			assert.Equal(t, tt.wantKey, hw.last.SystemUUID)
			require.NotNil(t, hw.last.AMTAvailable)
			assert.True(t, *hw.last.AMTAvailable)
			assert.Equal(t, "16.1.30.2260", hw.last.AMTVersion)
		})
	}
}

// TestHandleHardwareReportFromSilentAgent covers version skew: an agent that
// predates AMT reporting sends none of the three fields, and the nil presence
// flag is what lets the repository preserve what it already knows.
func TestHandleHardwareReportFromSilentAgent(t *testing.T) {
	t.Parallel()
	hw := &recordingHardware{}
	conn := &AgentConn{DeviceID: uuid.New(), hardware: hw, logger: testLogger()}

	require.NoError(t, conn.handleHardwareReport(context.Background(), &protocol.ControlMessage{
		Type:     protocol.MsgHardwareReport,
		CPUModel: "Intel Core i7-12700K",
	}))

	require.NotNil(t, hw.last)
	assert.Nil(t, hw.last.SystemUUID)
	assert.Nil(t, hw.last.AMTAvailable, "an absent flag must stay absent, not decode as a stated false")
	assert.Empty(t, hw.last.AMTVersion)
}

// recordingHardware captures the last hardware row written.
type recordingHardware struct{ last *device.Hardware }

func (r *recordingHardware) Upsert(_ context.Context, hw *device.Hardware) error {
	r.last = hw
	return nil
}

func (r *recordingHardware) Get(context.Context, device.DeviceID) (*device.Hardware, error) {
	return nil, device.ErrHardwareNotFound
}

func (r *recordingHardware) ResolveBySystemUUID(context.Context, uuid.UUID) (device.DeviceID, uuid.UUID, error) {
	return uuid.Nil, uuid.Nil, device.ErrHardwareNotFound
}

func (r *recordingHardware) SetAMTDetail(context.Context, device.DeviceID, string, string) error {
	return nil
}
