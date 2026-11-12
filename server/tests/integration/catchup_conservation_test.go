package integration

import (
	"context"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

// catchUpBatchesPerSession is how many batches each machine sends before it disconnects; one
// at a time, each answered before the next, as the machine sends them.
const catchUpBatchesPerSession = 3

// catchUpReadingsPerBatch is an hour of one dimension at one reading a minute.
const catchUpReadingsPerBatch = 60

// catchUpGoroutineSlopeTolerance is the goroutines per finished catch-up session the fit may
// carry; a goroutine timing each batch's queue wait read 3 per session, and the writer reads 0.
const catchUpGoroutineSlopeTolerance = 0.5

// catchUpHeapSlopeTolerance is the retained bytes per finished catch-up session the fit may carry;
// a settled batch kept with its answer read 158 KiB, and the writer reads 1.2 to 2.5 KiB.
const catchUpHeapSlopeTolerance = 8 << 10

func TestCatchUpSessionsConserveGoroutinesAndHeap(t *testing.T) {
	env := newCatchUpEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	site := testutil.SeedSite(t, ctx, env.store)

	// The first sessions grow the pools and compile queries, so they run before measurement.
	for range 3 {
		env.runCatchUpSession(t, ctx, site.ID)
	}
	env.settle(t)

	var xs, goroutines, heap []float64
	completed := 0
	for _, point := range conservationPoints {
		for ; completed < point; completed++ {
			env.runCatchUpSession(t, ctx, site.ID)
		}
		liveGoroutines, liveHeap := env.settle(t)
		xs = append(xs, float64(point))
		goroutines = append(goroutines, float64(liveGoroutines))
		heap = append(heap, float64(liveHeap))
	}

	goroutineSlope := slopeThrough(xs, goroutines)
	heapSlope := slopeThrough(xs, heap)
	t.Logf("finished catch-up sessions %v → goroutines %v (%.3f/session), heap %v (%.0f B/session)",
		xs, goroutines, goroutineSlope, heap, heapSlope)
	assert.Equal(t, int64((completed+3)*catchUpBatchesPerSession*catchUpReadingsPerBatch), env.stored.Load(),
		"every reading of every batch reached the store")

	assert.LessOrEqualf(t, math.Abs(goroutineSlope), catchUpGoroutineSlopeTolerance,
		"a finished catch-up session must give back its goroutines: %.3f retained per session", goroutineSlope)
	assert.LessOrEqualf(t, math.Abs(heapSlope), float64(catchUpHeapSlopeTolerance),
		"a finished catch-up session must give back its heap: %.0f bytes retained per session", heapSlope)
}

// catchUpEnv is one agent server whose readings go through the shared writer to a store that
// counts what it is sent.
type catchUpEnv struct {
	*agentTestEnv
	writer *telemetry.Writer
	stored atomic.Int64
}

func newCatchUpEnv(t *testing.T) *catchUpEnv {
	t.Helper()
	env := &catchUpEnv{agentTestEnv: &agentTestEnv{}}
	vm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || r.URL.Path != "/api/v1/import/prometheus" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		env.stored.Add(int64(strings.Count(string(body), "\n")))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(vm.Close)

	cfg := telemetry.DefaultWriterConfig()
	cfg.Store = telemetry.NewVMClient(vm.URL, nil)
	// A batch is answered once its request is, so a short flush age keeps each session brief.
	cfg.FlushAge = 10 * time.Millisecond
	env.writer = telemetry.NewWriter(cfg)

	store := testutil.NewTestStore(t)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	issuer := machinecert.NewIssuer(t, store, cm)
	srv := agentapi.NewAgentServer(agentapi.AgentServerConfig{
		Cert:          cm,
		Issuer:        issuer,
		Devices:       testutil.NewTestDevices(t, store),
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Telemetry:     env.writer,
		Relay:         relay.NewRelay(slog.Default()),
		Notifier:      &notifications.NoopNotifier{},
		Logger:        testLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	drained := env.writer.Start(ctx)
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	t.Cleanup(func() {
		cancel()
		<-drained
		<-listenDone
	})

	*env.agentTestEnv = agentTestEnv{
		store: store, devices: testutil.NewTestDevices(t, store), certMgr: cm, issuer: issuer,
		srv: srv, addr: srv.Addr(), cancel: cancel,
	}
	return env
}

// runCatchUpSession is one machine back from an outage: it connects, is granted catch-up, sends
// its batches one answer at a time, and disconnects.
func (e *catchUpEnv) runCatchUpSession(t *testing.T, ctx context.Context, siteID uuid.UUID) {
	t.Helper()
	deviceID := uuid.New()
	e.seedDevice(t, deviceID, siteID)
	tlsCert := machinecert.Issue(t, e.issuer, deviceID)
	conn, err := quic.DialAddr(ctx, e.addr, machinecert.ClientTLS(e.certMgr, tlsCert), &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	})
	require.NoError(t, err)
	defer conn.CloseWithError(0, "caught up")
	stream, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)

	performClientHandshake(t, stream, tlsCert.Certificate[0])
	sendControl(t, stream, &protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapHardwareInventory, protocol.CapBackfill},
		Hostname:     "catch-up-host",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	})
	drainRegisterHardwareRequest(t, stream)

	sendControl(t, stream, &protocol.ControlMessage{Type: protocol.MsgRequestBackfillSlot})
	awaitControl(t, stream, protocol.MsgGrantBackfill)

	start := time.Now().UTC().Truncate(time.Minute).Add(-time.Duration(catchUpBatchesPerSession) * time.Hour)
	for batch := range catchUpBatchesPerSession {
		samples := make([]protocol.BackfillSample, catchUpReadingsPerBatch)
		for i := range samples {
			at := start.Add(time.Duration(batch)*time.Hour + time.Duration(i)*time.Minute)
			samples[i] = protocol.BackfillSample{Name: "cpu.total", TS: at.Unix(), Value: float64(i)}
		}
		cursor := samples[len(samples)-1].TS
		sendControl(t, stream, &protocol.ControlMessage{
			Type:            protocol.MsgMetricBackfillBatch,
			Tier:            protocol.BackfillTierRecent60s,
			BackfillSamples: samples,
			Cursor:          cursor,
		})
		ack := awaitControl(t, stream, protocol.MsgMetricBackfillAck)
		require.Equal(t, cursor, ack.Cursor, "the answer names the batch it confirms")
	}
}

// settle waits for the server to book out every machine and the writer to hold nothing, then
// reads the process.
func (e *catchUpEnv) settle(t *testing.T) (goroutines int, heapBytes uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		return e.srv.ConnectedAgentCount() == 0 && e.writer.Fill(telemetry.ClassCatchUp) == 0
	}, 20*time.Second, 20*time.Millisecond, "the server must book out every machine that caught up")
	return settleProcess(t)
}

func sendControl(t *testing.T, stream *quic.Stream, msg *protocol.ControlMessage) {
	t.Helper()
	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, payload))
}

// awaitControl reads past every other message until one of type want arrives.
func awaitControl(t *testing.T, stream *quic.Stream, want protocol.ControlMessageType) *protocol.ControlMessage {
	t.Helper()
	require.NoError(t, stream.SetReadDeadline(time.Now().Add(10*time.Second)))
	defer func() { _ = stream.SetReadDeadline(time.Time{}) }()
	codec := &protocol.Codec{}
	for {
		frameType, payload, err := codec.ReadFrame(stream)
		require.NoError(t, err, "waiting for %s", want)
		if frameType != protocol.FrameControl {
			continue
		}
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err)
		if msg.Type == want {
			return msg
		}
	}
}
