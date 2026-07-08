package agentapi

import (
	"bytes"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
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
	groupID := uuid.New()
	var buf bytes.Buffer
	logger := testLogger()

	ac := NewAgentConn(AgentConnConfig{
		DeviceID:      deviceID,
		GroupID:       groupID,
		Stream:        &buf,
		Devices:       testutil.NewTestDevices(t, store),
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Logger:        logger,
	})
	assert.Equal(t, deviceID, ac.DeviceID)
	assert.Equal(t, groupID, ac.GroupID)
	assert.NotNil(t, ac.codec)
	assert.NotNil(t, ac.stream)
}
