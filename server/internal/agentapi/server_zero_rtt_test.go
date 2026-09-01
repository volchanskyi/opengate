package agentapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Early data is what a returning machine may send before the handshake it is
// running has finished. Anyone who can copy those packets can send them again,
// so this listener does not take them: a reconnect resumes its TLS session —
// skipping the asymmetric handshake — and still says nothing until the
// handshake is complete.
//
// The listener's QUIC configuration holds that by leaving the setting out, and
// a setting that is absent is one line away from being present. So the guard
// dials the running listener the way a machine that wanted early data would,
// and reads back what it was actually granted.
//
// One rule holds for every connection dialled this way: touch nothing on it
// until HandshakeComplete has fired. The connection is handed back while the
// handshake is still running, and the peer's transport settings are written by
// the same internal loop that opening a stream or reading connection state
// would read them from, with nothing guarding the two against each other.
// Receiving on HandshakeComplete first puts the read after the write. It costs
// this test nothing: whether early data was used is settled by the handshake
// itself, not by writing on a stream.
func TestProductionListenerRefusesEarlyData(t *testing.T) {
	env := newAcceptEnv(t)

	// One machine: one certificate and one session cache held across both
	// attempts. The cache is what carries the ticket, and the ticket is what
	// would carry an early-data allowance if the listener granted one.
	tlsCert, err := env.srv.cert.SignAgent(uuid.NewString(), "early-data-test")
	require.NoError(t, err)
	cache := newSignalingCache()
	tlsCfg := env.srv.cert.AgentTLSConfig(tlsCert)
	tlsCfg.ClientSessionCache = cache

	// The first connection banks the ticket. waitForTicket fails the test when
	// the listener never issues one — without a ticket no reconnect can resume
	// at all, and everything below would be asserted about nothing.
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
