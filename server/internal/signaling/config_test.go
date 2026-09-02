package signaling

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A browser that is handed no ICE server cannot try a direct connection at all,
// so the default has to name one rather than leaving the list to a deployment
// that may never set it.
func TestDefaultConfigNamesAServerToTry(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	require.Len(t, cfg.ICEServers, 1)
	require.Len(t, cfg.ICEServers[0].URLs, 1)
	assert.Equal(t, "stun:stun.l.google.com:19302", cfg.ICEServers[0].URLs[0])
	assert.Empty(t, cfg.ICEServers[0].Username, "a public STUN server takes no credential")
	assert.Empty(t, cfg.ICEServers[0].Credential)
}
