package integration

import (
	"context"
	"io"
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"nhooyr.io/websocket"
)

// conservationPoints are the completed-session counts the slope is fitted through; wide, even
// spacing keeps a one-off late allocation from reading as per-session retention.
var conservationPoints = []int{10, 20, 30, 40, 50}

// goroutineSlopeTolerance is the goroutines per completed session the fit may carry; the leak
// read 2 per session, and half a goroutine clears what a loaded machine adds between readings.
const goroutineSlopeTolerance = 0.5

// heapSlopeTolerance is the retained bytes per completed session the fit may carry; the leak
// read 34 KiB per session and a fixed server reads 1.2 to 2.0 KiB.
const heapSlopeTolerance = 8 << 10

func TestRelaySessionsConserveGoroutinesAndHeap(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	user := testutil.SeedUser(t, ctx, env.store)
	site := testutil.SeedSite(t, ctx, env.store)
	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	// One machine serves every session so a fresh QUIC peer's retention stays out of the fit.
	stream, deviceID := env.connectAgent(t, site.ID)
	require.Eventually(t, func() bool {
		d, err := device.NewPostgresDevices(env.store.DB()).Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 10*time.Second, 50*time.Millisecond, "the machine must be online before sessions are opened to it")

	// The first session grows the pool and compiles queries, so it runs before measurement.
	env.runCompleteRelaySession(t, ctx, stream, jwtToken, deviceID)
	env.settle(t)

	var xs, goroutines, heap []float64
	completed := 0
	for _, point := range conservationPoints {
		for ; completed < point; completed++ {
			env.runCompleteRelaySession(t, ctx, stream, jwtToken, deviceID)
		}
		liveGoroutines, liveHeap := env.settle(t)
		xs = append(xs, float64(point))
		goroutines = append(goroutines, float64(liveGoroutines))
		heap = append(heap, float64(liveHeap))
	}

	goroutineSlope := slopeThrough(xs, goroutines)
	heapSlope := slopeThrough(xs, heap)
	t.Logf("completed sessions %v → goroutines %v (%.3f/session), heap %v (%.0f B/session)",
		xs, goroutines, goroutineSlope, heap, heapSlope)

	assert.LessOrEqualf(t, math.Abs(goroutineSlope), goroutineSlopeTolerance,
		"a completed relay session must give back its goroutines: %.3f retained per session", goroutineSlope)
	assert.LessOrEqualf(t, math.Abs(heapSlope), float64(heapSlopeTolerance),
		"a completed relay session must give back its heap: %.0f bytes retained per session", heapSlope)
}

func (e *sessionTestEnv) runCompleteRelaySession(t *testing.T, ctx context.Context, stream io.ReadWriter, jwtToken string, deviceID uuid.UUID) {
	t.Helper()

	result := e.createSession(t, jwtToken, deviceID, map[string]bool{"desktop": true})

	codec := &protocol.Codec{}
	_, _, err := codec.ReadFrame(stream)
	require.NoError(t, err)
	acceptPayload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type:  protocol.MsgSessionAccept,
		Token: protocol.SessionToken(result.Token),
	})
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, acceptPayload))

	agentConn := e.dialRelayWS(t, ctx, result.Token, "agent", "")
	browserConn := e.dialRelayWS(t, ctx, result.Token, "browser", jwtToken)
	waitForRelayWired(t, ctx, e.relay, protocol.SessionToken(result.Token))

	require.NoError(t, agentConn.Write(ctx, websocket.MessageBinary, []byte("payload")))
	_, data, err := browserConn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, []byte("payload"), data)

	agentConn.Close(websocket.StatusNormalClosure, "done")
	browserConn.Close(websocket.StatusNormalClosure, "done")
}

// Teardown is asynchronous on both sides, so settle waits for the goroutine count to stop
// falling before it reads.
func (e *sessionTestEnv) settle(t *testing.T) (goroutines int, heapBytes uint64) {
	t.Helper()
	require.Eventually(t, func() bool { return e.relay.ActiveSessionCount() == 0 },
		20*time.Second, 20*time.Millisecond, "the relay must book out every finished session")

	last := runtime.NumGoroutine()
	stable := 0
	for i := 0; i < 200; i++ {
		time.Sleep(25 * time.Millisecond)
		runtime.GC()
		now := runtime.NumGoroutine()
		if now >= last {
			stable++
			if stable == 8 {
				break
			}
		} else {
			stable = 0
		}
		last = now
	}

	// The first collection runs finalizers that the second reclaims, so what remains is retained.
	runtime.GC()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return runtime.NumGoroutine(), stats.HeapAlloc
}

// slopeThrough fits y = a + bx and returns b; fewer than two distinct x values give zero.
func slopeThrough(xs, ys []float64) float64 {
	n := float64(len(xs))
	if n < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for i, x := range xs {
		sumX += x
		sumY += ys[i]
		sumXY += x * ys[i]
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}
