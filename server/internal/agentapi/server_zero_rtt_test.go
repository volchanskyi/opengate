package agentapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProductionListenerRefusesEarlyData(t *testing.T) {
	env := newAcceptEnv(t)

	// One certificate and one session cache span both attempts; the cache carries the ticket.
	tlsCert, err := env.srv.cert.SignAgent(uuid.NewString(), "early-data-test")
	require.NoError(t, err)
	cache := newSignalingCache()
	tlsCfg := env.srv.cert.AgentTLSConfig(tlsCert)
	tlsCfg.ClientSessionCache = cache

	// waitForTicket fails when the listener issues no ticket, since no reconnect could resume.
	firstConn, firstStream := env.dialWith(t, tlsCfg)
	readServerHello(t, firstStream)
	waitForTicket(t, cache)
	_ = firstConn.CloseWithError(0, "reconnecting")

	conn, err := quic.DialAddrEarly(t.Context(), env.addr, tlsCfg,
		&quic.Config{MaxIdleTimeout: 30 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })

	select {
	case <-conn.HandshakeComplete():
	case <-time.After(10 * time.Second):
		t.Fatal("the reconnect's handshake never completed")
	}

	state := conn.ConnectionState()
	assert.True(t, state.TLS.DidResume,
		"the machine came back on its ticket and the listener took it")
	assert.False(t, state.Used0RTT,
		"the listener granted early data — replayable bytes now reach it before the handshake finishes")
}
