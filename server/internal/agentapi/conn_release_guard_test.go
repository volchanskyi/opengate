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

// A connection the server has let go is not a connection anything may write
// down, and the transport cannot say so on its behalf.

// A handler resolves the connection it is going to push a request down, and the
// machine drops off between that lookup and the write. The write then goes into
// a send buffer nobody is reading and reports success, so the technician's
// screen says the restart was sent to a machine that has been gone for seconds.
//
// A QUIC stream cannot answer for its peer — a write to a socket whose far side
// has vanished succeeds — so the refusal is the server's own to make, from the
// one fact it holds: this connection has been released.
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

// The refusal is about this connection having been let go, not about the device
// — a machine that dropped and dialled straight back is served by the new
// connection, and the new one has been released by nothing.
func TestSendDownALiveConnectionIsNotRefused(t *testing.T) {
	t.Parallel()
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.setMeta("linux", "arm64", "0.1.0", []protocol.AgentCapability{protocol.CapHardwareInventory})

	require.NoError(t, ac.SendRequestHardwareReport(context.Background()))
	assert.NotZero(t, buf.Len())
}
