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

// Conservation: a completed operation gives back what it took.
//
// Every liveness number this server publishes is bookkeeping its teardown path
// maintains, not a reading of the resource. The relay's active-session gauge
// returned to zero correctly while the process held 7,455 goroutines, because
// the code that decrements the gauge ran. Coverage counted the leaking line as
// covered because it executed; the benchmark trend measures allocations per
// operation, which are identical for a leak because only retention differs; and
// a statement-level mutant of it is equivalent under every other assertion.
//
// So this file measures the resource itself, against the count of completed
// operations, and asserts the line through those points is flat.

// conservationPoints are the completed-session counts the slope is fitted
// through, cumulative against one server and one connected machine. Three
// points, because two cannot tell a slope from a single noisy reading. Small
// ones, because each session is an API call, a control-stream round trip and
// two real WebSocket dials.
var conservationPoints = []int{4, 8, 16}

// goroutineSlopeTolerance is the goroutines-per-completed-session the fit may
// carry. The defect that motivated this file retained two per session — one
// handler per side — so half a goroutine is far below a regression and far
// above what a loaded machine invents between two readings.
const goroutineSlopeTolerance = 0.5

// heapSlopeTolerance is the retained bytes per completed session the fit may
// carry. Driven through this harness, the same defect retained 34 KiB per
// session; a fixed server retains 1.8 KiB, which is the connection pool and the
// query cache growing rather than anything a session owns. 8 KiB sits between
// them with room on both sides.
const heapSlopeTolerance = 8 << 10

// TestRelaySessionsConserveGoroutinesAndHeap drives complete relay sessions
// against one assembled server and requires that neither goroutines nor heap
// grow with the number of them that have finished.
//
// A slope rather than a fixed baseline: the server, its store and its pool start
// goroutines that take no context and never stop, and a baseline would have to
// guess at that constant. A slope removes it. The method is the one
// server/tests/vmramseries uses, for the reason its own comment gives — a single
// reading divided by what is present answers a different question.
func TestRelaySessionsConserveGoroutinesAndHeap(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	user := testutil.SeedUser(t, ctx, env.store)
	site := testutil.SeedSite(t, ctx, env.store)
	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	// One machine for every session in the run. A fresh QUIC peer per session
	// would put its own retention into the fit, and the relay session is what
	// is being measured.
	stream, deviceID := env.connectAgent(t, site.ID)
	require.Eventually(t, func() bool {
		d, err := device.NewPostgresDevices(env.store.DB()).Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 10*time.Second, 50*time.Millisecond, "the machine must be online before sessions are opened to it")

	// Warm every path a session shares. The first one grows the pool and
	// compiles its queries, and charging that to session one would tilt the fit.
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

// runCompleteRelaySession opens one session to an already-connected machine,
// proves the pipe carries a frame, and hangs both sides up. One completed
// session — the unit the slopes above are measured per.
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

// settle waits for the relay to book every session out and for the goroutine
// count to stop falling, then returns the live goroutine count and heap.
// Teardown is asynchronous on both sides, so a reading taken the instant a
// client hangs up measures the tail of the last session rather than what the
// process is holding.
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

	// Two collections: the first runs finalizers that the second can then
	// reclaim, so what remains is retained rather than merely uncollected.
	runtime.GC()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return runtime.NumGoroutine(), stats.HeapAlloc
}

// slopeThrough fits y = a + bx and returns b. Fewer than two distinct x values
// describe no line; the caller's points are compile-time constants, so that
// case returns zero rather than reporting a slope it cannot know.
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
