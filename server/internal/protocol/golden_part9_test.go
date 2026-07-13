package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// WS-16 auto-discovery wire contract. The DiscoveryReport is Rust-encoded and
// decoded here for byte-level struct fidelity. It is additive and gated by the
// Discovery capability; the payload carries only bounded, secret-free inventory
// (engine/port/version — never connection strings or credentials).

func TestGoldenControlDiscoveryReport(t *testing.T) {
	msg := decodeControlFrame(t, "control_discovery_report.bin")
	assert.Equal(t, MsgDiscoveryReport, msg.Type)
	assert.Equal(t, int64(1700000000), msg.TS)
	assert.Empty(t, msg.OrgID, "agent must not assert an org")

	require.Len(t, msg.Ports, 2)
	assert.Equal(t, "tcp", msg.Ports[0].Proto)
	assert.Equal(t, uint16(5432), msg.Ports[0].Port)
	assert.Equal(t, "postgres", msg.Ports[0].Process)
	assert.Equal(t, "udp", msg.Ports[1].Proto)
	assert.Equal(t, uint16(53), msg.Ports[1].Port)
	assert.Equal(t, "systemd-resolve", msg.Ports[1].Process)

	require.Len(t, msg.Services, 1)
	assert.Equal(t, "nginx.service", msg.Services[0].Name)
	assert.Equal(t, "running", msg.Services[0].State)

	require.Len(t, msg.DBEngines, 1)
	assert.Equal(t, "postgres", msg.DBEngines[0].Engine)
	assert.Equal(t, "16.2", msg.DBEngines[0].Version)
	assert.Equal(t, uint16(5432), msg.DBEngines[0].Port)

	require.Len(t, msg.Containers, 1)
	assert.Equal(t, "docker", msg.Containers[0].Runtime)
	assert.Equal(t, "redis:7", msg.Containers[0].Image)
	assert.Equal(t, "cache", msg.Containers[0].Name)
	assert.Equal(t, "running", msg.Containers[0].State)

	require.Len(t, msg.Packages, 1)
	assert.Equal(t, "openssl", msg.Packages[0].Name)
	assert.Equal(t, "3.0.13", msg.Packages[0].Version)

	require.NotNil(t, msg.Truncated)
	assert.True(t, *msg.Truncated)
}
