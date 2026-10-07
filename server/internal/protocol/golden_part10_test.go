package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoldenControlMaintenanceApplied(t *testing.T) {
	msg := decodeControlFrame(t, "control_maintenance_applied.bin")
	assert.Equal(t, MsgMaintenanceApplied, msg.Type)
	require.NotNil(t, msg.Enabled, "enabled must decode as a present bool, not nil")
	assert.True(t, *msg.Enabled)
}
