package agentapi

import (
	"crypto/tls"
	"crypto/x509"
	"math"
	"os"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

// machineListener listens with the machine-facing transport settings over the server's own
// certificate, with no client certificate asked for, since only the transport is under test.
func machineListener(t *testing.T, budget *receiveBudget) (*quic.Listener, *tls.Config) {
	t.Helper()
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	serverTLS, err := cm.ServerTLSConfig()
	require.NoError(t, err)
	serverTLS = serverTLS.Clone()
	serverTLS.ClientAuth = tls.NoClientCert
	serverTLS.VerifyConnection = nil

	ln, err := quic.ListenAddr("127.0.0.1:0", serverTLS, machineQUICConfig(budget))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(cm.CACertPEM())
	clientTLS := &tls.Config{
		ServerName: "localhost",
		RootCAs:    pool,
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"opengate"},
	}
	return ln, clientTLS
}

func dialMachineListener(t *testing.T, ln *quic.Listener, clientTLS *tls.Config) *quic.Conn {
	t.Helper()
	conn, err := quic.DialAddr(t.Context(), ln.Addr().String(), clientTLS,
		&quic.Config{MaxIdleTimeout: 30 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })
	return conn
}

func TestAMachineTheServerHasNotReadParksOnlyItsOpeningWindow(t *testing.T) {
	budget := newReceiveBudget(8 << 20)
	ln, clientTLS := machineListener(t, budget)

	accepted := make(chan *quic.Stream, 1)
	go func() {
		conn, err := ln.Accept(t.Context())
		if err != nil {
			return
		}
		stream, err := conn.AcceptStream(t.Context())
		if err != nil {
			return
		}
		accepted <- stream
	}()

	conn := dialMachineListener(t, ln, clientTLS)
	stream, err := conn.OpenStreamSync(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.SetWriteDeadline(time.Now().Add(2*time.Second)))

	sent, err := stream.Write(make([]byte, maxMachineMessageBytes))

	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "a server that reads nothing stops the machine")
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("the server never saw the machine's stream")
	}
	assert.Positive(t, sent)
	assert.LessOrEqual(t, sent, machineOpeningWindow,
		"the transport holds no more than the opening window for a machine the server has not read")
	assert.True(t, budget.allow(&quic.Conn{}, 8<<20), "a window nobody read from never grows")
}

func TestAConnectionGrowsOnlyWhileTheSharedBudgetHasRoom(t *testing.T) {
	budget := newReceiveBudget(100)
	first, second := &quic.Conn{}, &quic.Conn{}

	assert.True(t, budget.allow(first, 60))
	assert.False(t, budget.allow(second, 60), "growth past the budget is refused")
	assert.True(t, budget.allow(second, 40))
	assert.False(t, budget.allow(first, 1), "a spent budget refuses every connection")
}

func TestAClosedConnectionHandsItsShareBack(t *testing.T) {
	budget := newReceiveBudget(64 << 10)
	ln, clientTLS := machineListener(t, budget)

	accepted := make(chan *quic.Conn, 1)
	go func() {
		conn, err := ln.Accept(t.Context())
		if err == nil {
			accepted <- conn
		}
	}()
	client := dialMachineListener(t, ln, clientTLS)
	var server *quic.Conn
	select {
	case server = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("the listener never accepted the machine")
	}
	budget.watch(server)
	require.True(t, budget.allow(server, 64<<10))
	other := &quic.Conn{}
	require.False(t, budget.allow(other, 1), "the budget is spent while the connection holds it")

	require.NoError(t, client.CloseWithError(0, "machine leaves"))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if budget.allow(other, 64<<10) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a closed connection kept its share")
}

func TestReleasingAConnectionThatNeverGrewChangesNothing(t *testing.T) {
	budget := newReceiveBudget(100)
	grown, idle := &quic.Conn{}, &quic.Conn{}
	require.True(t, budget.allow(grown, 30))

	budget.release(idle)
	budget.release(idle)

	probe := &quic.Conn{}
	assert.True(t, budget.allow(probe, 70), "a connection that never grew hands back nothing")
	assert.False(t, budget.allow(probe, 1))
	budget.release(probe)
	budget.release(grown)
	budget.release(grown)
	assert.True(t, budget.allow(probe, 100))
	assert.False(t, budget.allow(probe, 1), "a share is handed back once")
}

func TestTheReceiveBudgetIsAShareOfTheMemoryLimit(t *testing.T) {
	cases := []struct {
		name  string
		limit int64
		want  int64
	}{
		{"production's limit, 90% of 384 MiB", 362387865, 18119393},
		{"a small limit still grows one window to the largest message", 8 << 20, maxMachineMessageBytes},
		{"no limit set keeps the fixed budget", math.MaxInt64, unlimitedReceiveBudgetBytes},
		{"a limit that reads as nothing keeps the fixed budget", 0, unlimitedReceiveBudgetBytes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, receiveBudgetFor(tc.limit))
		})
	}
}

func TestTheMachineTransportOpensSmallAndGrowsThroughTheBudget(t *testing.T) {
	budget := newReceiveBudget(1 << 20)
	cfg := machineQUICConfig(budget)

	assert.Equal(t, uint64(machineOpeningWindow), cfg.InitialStreamReceiveWindow)
	assert.Equal(t, uint64(machineOpeningWindow), cfg.InitialConnectionReceiveWindow)
	assert.Equal(t, uint64(maxMachineMessageBytes), cfg.MaxStreamReceiveWindow)
	assert.Equal(t, uint64(maxMachineMessageBytes), cfg.MaxConnectionReceiveWindow)
	require.NotNil(t, cfg.AllowConnectionWindowIncrease)
	assert.True(t, cfg.AllowConnectionWindowIncrease(&quic.Conn{}, 1<<20))
	assert.False(t, cfg.AllowConnectionWindowIncrease(&quic.Conn{}, 1), "growth draws on the budget it was given")
	assert.Equal(t, 90*time.Second, cfg.MaxIdleTimeout, "the machine transport keeps its idle limit")
	assert.Equal(t, 30*time.Second, cfg.KeepAlivePeriod)
}
