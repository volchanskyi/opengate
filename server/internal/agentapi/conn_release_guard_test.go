package agentapi

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestSendAfterTheConnectionIsReleasedIsRefused(t *testing.T) {
	t.Parallel()
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.setMeta("linux", "arm64", "0.1.0", []protocol.AgentCapability{protocol.CapHardwareInventory})
	ac.markReleased()

	err := ac.SendRequestHardwareReport(context.Background())

	require.Error(t, err, "a send down a released connection must surface an error")
	assert.True(t, errors.Is(err, ErrConnectionClosed), "want ErrConnectionClosed, got %v", err)
	assert.Zero(t, buf.Len(), "no frame may reach a machine that has gone")
}

func TestSendDownALiveConnectionIsNotRefused(t *testing.T) {
	t.Parallel()
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.setMeta("linux", "arm64", "0.1.0", []protocol.AgentCapability{protocol.CapHardwareInventory})

	require.NoError(t, ac.SendRequestHardwareReport(context.Background()))
	assert.NotZero(t, buf.Len())
}
