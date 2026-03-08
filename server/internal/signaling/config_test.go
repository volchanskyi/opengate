package signaling

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	require.Len(t, cfg.ICEServers, 1)
	assert.Contains(t, cfg.ICEServers[0].URLs, "stun:stun.l.google.com:19302")
	assert.Empty(t, cfg.ICEServers[0].Username)
	assert.Empty(t, cfg.ICEServers[0].Credential)
	assert.Equal(t, 30*time.Second, cfg.UpgradeTimeout)
	assert.Equal(t, 10*time.Second, cfg.ICEGatherTimeout)
}

func TestICEServerWithCredentials(t *testing.T) {
	srv := ICEServer{
		URLs:       []string{"turn:turn.example.com:3478"},
		Username:   "user",
		Credential: "pass",
	}

	assert.Equal(t, "user", srv.Username)
	assert.Equal(t, "pass", srv.Credential)
}
