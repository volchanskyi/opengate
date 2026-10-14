package signaling

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReadsTheHostRelayAddressAndTheHelpers(t *testing.T) {
	t.Parallel()

	cfg, err := New("wss://opengate.example.com", " stun:quic.example.com:9090 , stun:10.0.0.5:3478,")
	require.NoError(t, err)

	assert.Equal(t, "wss://opengate.example.com/ws/relay/abc123", cfg.AgentRelayURL("abc123"))
	assert.Equal(t, []string{"stun:quic.example.com:9090", "stun:10.0.0.5:3478"}, cfg.Helpers)
}

func TestNewAcceptsAPlainWebSocketAddressAndNoHelpers(t *testing.T) {
	t.Parallel()

	cfg, err := New("ws://server:8080/", "")
	require.NoError(t, err)

	assert.Equal(t, "ws://server:8080/ws/relay/tok", cfg.AgentRelayURL("tok"))
	assert.Empty(t, cfg.Helpers, "an empty list is a valid setting: the sides try their local addresses")
	assert.NotNil(t, cfg.Helpers, "the list reaches both sides as an empty list, never as nothing")
}

func TestNewRefusesAMissingOrMalformedHostRelayAddress(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{
		"",
		"https://opengate.example.com",
		"wss://",
		"wss://opengate.example.com/ws/relay",
		"wss://opengate.example.com?x=1",
		"wss://user:pass@opengate.example.com",
		"opengate.example.com",
	} {
		_, err := New(bad, "")
		assert.Errorf(t, err, "%q must be refused", bad)
	}

	_, err := New("", "")
	require.ErrorIs(t, err, ErrAgentRelayRequired)
}

func TestNewRefusesAHelperThatIsNotAPlainSTUNAddress(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{
		"turn:relay.example.com:3478",
		"stuns:quic.example.com:5349",
		"stun:quic.example.com",
		"stun::9090",
		"stun:quic.example.com:0",
		"stun:quic.example.com:70000",
		"stun:quic.example.com:port",
		"quic.example.com:9090",
	} {
		_, err := New("wss://opengate.example.com", bad)
		assert.Errorf(t, err, "helper %q must be refused", bad)
	}
}
