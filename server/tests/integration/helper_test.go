package integration

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/stun/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/cert"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/stunhelper"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// helperTestEnv is the agent QUIC listener with the helper answering on its port.
type helperTestEnv struct {
	*agentTestEnv
	metrics *appmetrics.Metrics
}

func newHelperTestEnv(t *testing.T, limits stunhelper.Limits) *helperTestEnv {
	t.Helper()
	store := testutil.NewTestStore(t)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	metrics := appmetrics.NewMetrics(prometheus.NewRegistry())

	srv := agentapi.NewAgentServer(agentapi.AgentServerConfig{
		Cert:          cm,
		Devices:       testutil.NewTestDevices(t, store),
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Relay:         relay.NewRelay(slog.Default()),
		Notifier:      &notifications.NoopNotifier{},
		Metrics:       metrics,
		Helper:        stunhelper.New(limits),
		Logger:        testLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		_ = srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	addr := srv.Addr()
	t.Cleanup(func() {
		cancel()
		select {
		case <-listenDone:
		case <-time.After(2 * time.Second):
			t.Log("agent QUIC server did not exit within 2s of cancel")
		}
	})

	return &helperTestEnv{
		agentTestEnv: &agentTestEnv{
			store: store, certMgr: cm, srv: srv, addr: addr, cancel: cancel,
			devices: testutil.NewTestDevices(t, store),
		},
		metrics: metrics,
	}
}

// askFrom asks n questions from a socket bound to source the way a STUN client does, asking each
// unanswered one again after 500 ms, then twice as long each time, up to retries times.
func (e *helperTestEnv) askFrom(t *testing.T, source string, n, retries int) int {
	t.Helper()
	conn, err := net.ListenPacket("udp4", source+":0")
	require.NoError(t, err)
	defer conn.Close()
	server, err := net.ResolveUDPAddr("udp4", e.addr)
	require.NoError(t, err)
	local := conn.LocalAddr().(*net.UDPAddr)

	pending := map[[stun.TransactionIDSize]byte][]byte{}
	for range n {
		msg, err := stun.Build(stun.TransactionID, stun.BindingRequest)
		require.NoError(t, err)
		pending[msg.TransactionID] = append([]byte(nil), msg.Raw...)
	}

	answered := 0
	buf := make([]byte, 1500)
	wait := 500 * time.Millisecond
	for range 1 + retries {
		for _, raw := range pending {
			_, err := conn.WriteTo(raw, server)
			require.NoError(t, err)
		}
		deadline := time.Now().Add(wait)
		wait *= 2
		for len(pending) > 0 {
			require.NoError(t, conn.SetReadDeadline(deadline))
			m, _, err := conn.ReadFrom(buf)
			if err != nil {
				break
			}
			reply := stun.Message{Raw: append([]byte(nil), buf[:m]...)}
			require.NoError(t, reply.Decode())
			var mapped stun.XORMappedAddress
			require.NoError(t, mapped.GetFrom(&reply))
			require.True(t, mapped.IP.Equal(local.IP), "the reply names the asker's own address")
			require.Equal(t, local.Port, mapped.Port)
			if _, open := pending[reply.TransactionID]; open {
				delete(pending, reply.TransactionID)
				answered++
			}
		}
		if len(pending) == 0 {
			break
		}
	}
	return answered
}

// connectMachines connects n machines over QUIC and returns their device ids.
func (e *helperTestEnv) connectMachines(t *testing.T, n int) []uuid.UUID {
	t.Helper()
	site := testutil.SeedSite(t, defaultTenantContext(), e.store)
	ids := make([]uuid.UUID, n)
	for i := range ids {
		_, ids[i] = e.connectAgent(t, site.ID)
	}
	return ids
}

func (e *helperTestEnv) requireAllConnected(t *testing.T, ids []uuid.UUID) {
	t.Helper()
	for _, id := range ids {
		require.NotNilf(t, e.srv.GetAgent(id), "machine %s stays connected beside the helper", id)
	}
}

func TestTheHelperAnswersAWholeBurstOfSessionStartsBesideConnectedMachines(t *testing.T) {
	t.Parallel()
	env := newHelperTestEnv(t, stunhelper.DefaultLimits)
	machines := env.connectMachines(t, 3)

	// Fifty attempts, each two sides asking from up to three interfaces, inside one second.
	const attempts, asksPerAttempt = 50, 6
	var wg sync.WaitGroup
	answered := make([]int, attempts)
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(i) * time.Second / attempts)
			answered[i] = env.askFrom(t, fmt.Sprintf("127.0.%d.%d", 1+i/200, 1+i%200), asksPerAttempt, 2)
		}()
	}
	wg.Wait()

	total := 0
	for _, n := range answered {
		total += n
	}
	assert.Equal(t, attempts*asksPerAttempt, total, "every request of a legitimate burst is answered")
	env.requireAllConnected(t, machines)
}

func TestAFloodFromOneAddressStopsAtItsAllowanceAndOthersAreStillAnswered(t *testing.T) {
	t.Parallel()
	limits := stunhelper.Limits{PerAddress: 20, Total: 10_000, Slots: 4096}
	env := newHelperTestEnv(t, limits)
	machines := env.connectMachines(t, 2)

	flooded := env.askFrom(t, "127.0.0.21", 200, 0)
	assert.LessOrEqual(t, flooded, 2*limits.PerAddress,
		"one address is answered no more than its allowance in each second the flood spans")
	assert.Equal(t, 5, env.askFrom(t, "127.0.0.22", 5, 2), "another address keeps being answered")
	env.requireAllConnected(t, machines)
}

func TestAFloodFromManyAddressesStopsAtTheTotal(t *testing.T) {
	t.Parallel()
	limits := stunhelper.Limits{PerAddress: 50, Total: 100, Slots: 64}
	env := newHelperTestEnv(t, limits)
	machines := env.connectMachines(t, 2)

	var wg sync.WaitGroup
	var mu sync.Mutex
	answered := 0
	for i := range 400 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := env.askFrom(t, fmt.Sprintf("127.1.%d.%d", 1+i/200, 1+i%200), 1, 0)
			mu.Lock()
			answered += n
			mu.Unlock()
		}()
	}
	wg.Wait()

	assert.LessOrEqual(t, answered, 3*limits.Total,
		"answers stop at the total in each second the flood spans, however many addresses ask")
	assert.Positive(t, answered)
	env.requireAllConnected(t, machines)
}

// paramsTrace records every event of one connection, for reading the peer's transport parameters.
type paramsTrace struct {
	mu     sync.Mutex
	events []qlogwriter.Event
}

func (p *paramsTrace) AddProducer() qlogwriter.Recorder { return p }
func (p *paramsTrace) SupportsSchemas(string) bool      { return true }
func (p *paramsTrace) Close() error                     { return nil }
func (p *paramsTrace) RecordEvent(e qlogwriter.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
}

func TestTheAgentListenerNeverOffersToLetPeersClearTheFixedBit(t *testing.T) {
	t.Parallel()
	env := newHelperTestEnv(t, stunhelper.DefaultLimits)

	trace := &paramsTrace{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tlsCert, err := env.certMgr.SignAgent(uuid.New().String(), "fixed-bit-probe")
	require.NoError(t, err)
	tlsCfg := env.certMgr.AgentTLSConfig(tlsCert)
	tlsCfg = tlsCfg.Clone()
	tlsCfg.MinVersion = tls.VersionTLS13
	conn, err := quic.DialAddr(ctx, env.addr, tlsCfg, &quic.Config{
		Tracer: func(context.Context, bool, quic.ConnectionID) qlogwriter.Trace { return trace },
	})
	require.NoError(t, err)
	defer conn.CloseWithError(0, "probe done")

	trace.mu.Lock()
	defer trace.mu.Unlock()
	var serverParams *qlog.ParametersSet
	for _, e := range trace.events {
		if set, ok := e.(qlog.ParametersSet); ok && set.Initiator == qlog.InitiatorRemote {
			serverParams = &set
		}
	}
	require.NotNil(t, serverParams, "the probe must read the listener's transport parameters")

	params := reflect.ValueOf(*serverParams)
	for i := range params.NumField() {
		name := params.Type().Field(i).Name
		if strings.Contains(strings.ToLower(name), "grease") {
			assert.Falsef(t, params.Field(i).IsValid() && !params.Field(i).IsZero(),
				"the listener offers %s, which lets a machine clear the fixed bit and its packets "+
					"reach the helper instead of QUIC", name)
		}
	}
}
