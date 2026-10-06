package signaling

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfigNamesAServerToTry(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	require.Len(t, cfg.ICEServers, 1)
	require.Len(t, cfg.ICEServers[0].URLs, 1)
	assert.Equal(t, "stun:stun.l.google.com:19302", cfg.ICEServers[0].URLs[0])
	assert.Empty(t, cfg.ICEServers[0].Username, "a public STUN server takes no credential")
	assert.Empty(t, cfg.ICEServers[0].Credential)
}
