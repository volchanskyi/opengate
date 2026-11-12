package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// connectedMachine registers a machine on srv whose writes land in a recorded stream.
func connectedMachine(t *testing.T, srv *AgentServer, caps ...protocol.AgentCapability) (*AgentConn, *sentStream) {
	t.Helper()
	stream := &sentStream{}
	ac := &AgentConn{DeviceID: uuid.New(), stream: stream, codec: &protocol.Codec{}, logger: testLogger()}
	ac.setMeta("linux", "amd64", "0.1.0", caps)
	srv.registerConn(context.Background(), ac, "contoso-desk")
	return ac, stream
}

// resends decodes the re-send requests a machine was sent.
func resends(t *testing.T, stream *sentStream) []*protocol.ControlMessage {
	t.Helper()
	msgs, err := stream.controls()
	require.NoError(t, err)
	var out []*protocol.ControlMessage
	for _, msg := range msgs {
		if msg.Type == protocol.MsgDeferBackfill && msg.SinceTS != 0 {
			out = append(out, msg)
		}
	}
	return out
}

func newResendServer(t *testing.T) (*AgentServer, *appmetrics.Metrics) {
	t.Helper()
	srv := newTestAgentServer(t)
	metrics := appmetrics.NewMetrics(appmetrics.NewRegistry())
	srv.metrics = metrics
	return srv, metrics
}

func TestAStoreRestartAsksEveryMachineThatReadsTheRequestToSendAgain(t *testing.T) {
	srv, metrics := newResendServer(t)
	var current []*sentStream
	for range 3 {
		_, stream := connectedMachine(t, srv, protocol.CapBackfill, protocol.CapResendRequests)
		current = append(current, stream)
	}
	_, oldBuild := connectedMachine(t, srv, protocol.CapBackfill)
	since := time.Unix(1_763_000_000, 0)

	srv.RequestResendAfterRestart(since)

	var retries []uint32
	for _, stream := range current {
		asked := resends(t, stream)
		require.Len(t, asked, 1, "each machine is asked once")
		assert.Equal(t, since.Unix(), asked[0].SinceTS, "from thirty seconds before the restart's last read")
		retries = append(retries, asked[0].RetryAfter)
	}
	assert.Empty(t, resends(t, oldBuild), "a machine on an old build is never sent a re-send request")
	for _, retry := range retries {
		assert.GreaterOrEqual(t, retry, uint32(1))
		assert.LessOrEqual(t, retry, uint32(60), "the requests are spread over a minute")
	}
	assert.NotEqual(t, retries[0], retries[2], "the fleet does not come back all at once")
	assert.InDelta(t, 1, promtestutil.ToFloat64(metrics.IngestStoreRestartsTotal), 0)
	assert.InDelta(t, 3, promtestutil.ToFloat64(metrics.IngestResendRequestsTotal.WithLabelValues("store_restart")), 0)
}

func TestAMachineOfflineDuringARestartIsAskedOnceAtItsNextConnect(t *testing.T) {
	srv, _ := newResendServer(t)
	since := time.Unix(1_763_000_000, 0)
	srv.RequestResendAfterRestart(since)

	returning, stream := connectedMachine(t, srv, protocol.CapBackfill, protocol.CapResendRequests)
	srv.resendOnConnect(returning)
	srv.resendOnConnect(returning)

	asked := resends(t, stream)
	require.Len(t, asked, 1, "one crash asks a machine once, however often it reconnects")
	assert.Equal(t, since.Unix(), asked[0].SinceTS)
}

func TestAMachineThatConnectsWithNoRestartBehindItIsAskedNothing(t *testing.T) {
	srv, _ := newResendServer(t)
	machine, stream := connectedMachine(t, srv, protocol.CapBackfill, protocol.CapResendRequests)
	srv.resendOnConnect(machine)
	assert.Empty(t, resends(t, stream))
}

func TestMachinesWhoseLiveMinutesWereDroppedAreAskedFromTheFirstDroppedMinute(t *testing.T) {
	srv, metrics := newResendServer(t)
	dropped, droppedStream := connectedMachine(t, srv, protocol.CapBackfill, protocol.CapResendRequests)
	_, quietStream := connectedMachine(t, srv, protocol.CapBackfill, protocol.CapResendRequests)
	oldBuild, oldStream := connectedMachine(t, srv, protocol.CapBackfill)
	minute := time.Unix(1_763_000_040, 0)

	srv.resendLiveDrops(map[uuid.UUID]time.Time{
		dropped.DeviceID: minute, oldBuild.DeviceID: minute, uuid.New(): minute,
	})

	asked := resends(t, droppedStream)
	require.Len(t, asked, 1)
	assert.Equal(t, minute.Unix(), asked[0].SinceTS)
	assert.Empty(t, resends(t, quietStream), "a machine that lost nothing hears nothing")
	assert.Empty(t, resends(t, oldStream))
	assert.InDelta(t, 1, promtestutil.ToFloat64(metrics.IngestResendRequestsTotal.WithLabelValues("live_drop")), 0)
}

func TestTheLiveDropLoopAsksAsTheWriterReportsAndStopsWithItsContext(t *testing.T) {
	srv, _ := newResendServer(t)
	dropped, stream := connectedMachine(t, srv, protocol.CapBackfill, protocol.CapResendRequests)
	minute := time.Unix(1_763_000_040, 0)
	reports := make(chan map[uuid.UUID]time.Time, 1)
	reports <- map[uuid.UUID]time.Time{dropped.DeviceID: minute}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		srv.RunLiveDropResends(ctx, func() map[uuid.UUID]time.Time {
			select {
			case drops := <-reports:
				return drops
			default:
				return nil
			}
		}, time.Millisecond)
	}()

	require.Eventually(t, func() bool { return len(resends(t, stream)) == 1 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-stopped
}
