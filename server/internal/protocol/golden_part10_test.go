package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Maintenance mode wire contract. SetMaintenanceMode (server → agent) is a
// reverse golden verified on the Rust side; MaintenanceApplied (agent → server)
// is Rust-encoded and decoded here for byte-level struct fidelity. Both carry a
// single bool that survives round-trip regardless of value.

func TestGoldenControlMaintenanceApplied(t *testing.T) {
	msg := decodeControlFrame(t, "control_maintenance_applied.bin")
	assert.Equal(t, MsgMaintenanceApplied, msg.Type)
	require.NotNil(t, msg.Enabled, "enabled must decode as a present bool, not nil")
	assert.True(t, *msg.Enabled)
}
