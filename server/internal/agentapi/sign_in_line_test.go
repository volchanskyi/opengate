package agentapi

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	contosoCustomer  = uuid.MustParse("c0000000-0000-4000-8000-000000000001")
	fabrikamCustomer = uuid.MustParse("f0000000-0000-4000-8000-000000000002")
)

// newTestLine is a line running inProgress at once that holds capacity waiting until it has
// measured a pace of its own.
func newTestLine(inProgress, capacity int) *signInLine {
	return newSignInLine(inProgress, float64(capacity)/signInLineSeconds, time.Now)
}

// fakeClock moves only when told.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// lineWaitBudget bounds every wait on a line that should move at once.
const lineWaitBudget = 5 * time.Second

// queued is one sign-in waiting in the line, and what its wait came to.
type queued struct {
	name   string
	result chan error
	leave  func()
}

// joinLine starts a sign-in for the customer and returns once the line counts it waiting.
func joinLine(t *testing.T, line *signInLine, ctx context.Context, name string, customer uuid.UUID) *queued {
	t.Helper()
	before := line.waiting()
	return startSignIn(t, line, ctx, name, customer, func() bool { return line.waiting() == before+1 })
}

// startSignIn starts a sign-in for the customer and returns once arrived holds or it has an answer.
func startSignIn(t *testing.T, line *signInLine, ctx context.Context, name string, customer uuid.UUID,
	arrived func() bool,
) *queued {
	t.Helper()
	q := &queued{name: name, result: make(chan error, 1)}
	go func() {
		leave, err := line.enter(ctx, customer)
		q.leave = leave
		q.result <- err
	}()
	require.Eventually(t, func() bool { return arrived() || len(q.result) > 0 },
		lineWaitBudget, time.Millisecond, "%s never reached the line", name)
	return q
}

// admitted waits for one of the sign-ins to get in and returns it.
func admitted(t *testing.T, candidates ...*queued) *queued {
	t.Helper()
	deadline := time.After(lineWaitBudget)
	for {
		for _, q := range candidates {
			select {
			case err := <-q.result:
				require.NoError(t, err, "%s was refused where it should have got in", q.name)
				return q
			default:
			}
		}
		select {
		case <-deadline:
			t.Fatal("nobody waiting got in")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestASignInWithRoomStartsAtOnce(t *testing.T) {
	t.Parallel()
	line := newTestLine(2, 10)

	first, err := line.enter(context.Background(), contosoCustomer)
	require.NoError(t, err)
	second, err := line.enter(context.Background(), fabrikamCustomer)
	require.NoError(t, err)

	assert.Zero(t, line.waiting())
	first()
	second()
}

func TestAWaitingSignInStartsWhenOneInProgressEnds(t *testing.T) {
	t.Parallel()
	line := newTestLine(1, 10)
	holder, err := line.enter(context.Background(), contosoCustomer)
	require.NoError(t, err)

	next := joinLine(t, line, context.Background(), "next", contosoCustomer)
	assert.Empty(t, next.result, "a full line holds the next sign-in")

	holder()
	assert.Same(t, next, admitted(t, next))
	assert.Zero(t, line.waiting())
}

func TestWaitingCustomersTakeTurns(t *testing.T) {
	t.Parallel()
	line := newTestLine(1, 10)
	holder, err := line.enter(context.Background(), contosoCustomer)
	require.NoError(t, err)

	contoso1 := joinLine(t, line, context.Background(), "contoso-1", contosoCustomer)
	contoso2 := joinLine(t, line, context.Background(), "contoso-2", contosoCustomer)
	contoso3 := joinLine(t, line, context.Background(), "contoso-3", contosoCustomer)
	fabrikam1 := joinLine(t, line, context.Background(), "fabrikam-1", fabrikamCustomer)
	unknown1 := joinLine(t, line, context.Background(), "new-machine-1", uuid.Nil)

	var order []string
	leave := holder
	pending := []*queued{contoso1, contoso2, contoso3, fabrikam1, unknown1}
	for len(pending) > 0 {
		leave()
		in := admitted(t, pending...)
		order = append(order, in.name)
		leave = in.leave
		pending = without(pending, in)
	}
	leave()

	assert.Equal(t, []string{"contoso-1", "fabrikam-1", "new-machine-1", "contoso-2", "contoso-3"}, order,
		"each customer with machines waiting gets the next turn in the order it first waited")
}

func TestAFullLineTurnsAwayTheNewestOfTheCustomerHoldingMostOfIt(t *testing.T) {
	t.Parallel()
	line := newTestLine(1, 3)
	holder, err := line.enter(context.Background(), contosoCustomer)
	require.NoError(t, err)
	contoso1 := joinLine(t, line, context.Background(), "contoso-1", contosoCustomer)
	contoso2 := joinLine(t, line, context.Background(), "contoso-2", contosoCustomer)
	contoso3 := joinLine(t, line, context.Background(), "contoso-3", contosoCustomer)

	fabrikam1 := startSignIn(t, line, context.Background(), "fabrikam-1", fabrikamCustomer,
		func() bool { return len(contoso3.result) > 0 })

	require.ErrorIs(t, <-contoso3.result, errComeBackLater,
		"Contoso held the whole line, so its newest gives way")
	assert.Empty(t, fabrikam1.result, "Fabrikam's machine keeps its place")
	assert.Equal(t, 3, line.waiting())

	_, err = line.enter(context.Background(), contosoCustomer)
	require.ErrorIs(t, err, errComeBackLater,
		"a newcomer whose customer already holds the most of a full line is the one turned away")

	holder()
	first := admitted(t, contoso1, contoso2, fabrikam1)
	assert.Same(t, contoso1, first)
	first.leave()
	second := admitted(t, contoso2, fabrikam1)
	assert.Same(t, fabrikam1, second, "the turn passes to Fabrikam")
	second.leave()
	admitted(t, contoso2).leave()
}

func TestAMachineThatHangsUpWhileWaitingLeavesTheLine(t *testing.T) {
	t.Parallel()
	line := newTestLine(1, 10)
	holder, err := line.enter(context.Background(), contosoCustomer)
	require.NoError(t, err)

	ctx, hangUp := context.WithCancel(context.Background())
	waiter := joinLine(t, line, ctx, "waiter", contosoCustomer)
	hangUp()

	select {
	case err := <-waiter.result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(lineWaitBudget):
		t.Fatal("a machine that hung up stayed in the line")
	}
	assert.Zero(t, line.waiting())

	holder()
	again, err := line.enter(context.Background(), fabrikamCustomer)
	require.NoError(t, err, "the slot the holder gave back is free, not handed to the machine that left")
	again()
}

func TestLeavingTwiceGivesBackOneSlot(t *testing.T) {
	t.Parallel()
	line := newTestLine(1, 10)
	leave, err := line.enter(context.Background(), contosoCustomer)
	require.NoError(t, err)

	leave()
	leave()

	first, err := line.enter(context.Background(), contosoCustomer)
	require.NoError(t, err)
	second := joinLine(t, line, context.Background(), "second", contosoCustomer)
	assert.Empty(t, second.result, "one slot came back, not two")
	first()
	admitted(t, second).leave()
}

func TestALineThatNeverFilledHoldsThirtySecondsOfThePromisedRate(t *testing.T) {
	t.Parallel()

	_, quarter := signInLineSize(0.25)
	_, whole := signInLineSize(1)

	assert.Equal(t, 3000, newSignInLine(1, quarter, time.Now).capacity(),
		"a quarter processor promises 100 sign-ins a second")
	assert.Equal(t, 12000, newSignInLine(1, whole, time.Now).capacity())
}

// serveAtPace holds the line's one slot, seats twelve machines behind it, and hands the slot on
// every interval of the clock until all have signed in.
func serveAtPace(t *testing.T, line *signInLine, clock *fakeClock, interval time.Duration) {
	t.Helper()
	holder, err := line.enter(context.Background(), contosoCustomer)
	require.NoError(t, err)
	var waiting []*queued
	for range 12 {
		waiting = append(waiting, joinLine(t, line, context.Background(), "contoso", contosoCustomer))
	}
	leave := holder
	for len(waiting) > 0 {
		clock.advance(interval)
		leave()
		in := admitted(t, waiting...)
		waiting = without(waiting, in)
		leave = in.leave
	}
	leave()
}

func TestALineHoldsThirtySecondsOfThePaceItKeptWhileMachinesWaited(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	line := newSignInLine(1, 100, clock.read)

	serveAtPace(t, line, clock, 500*time.Millisecond)

	assert.Equal(t, 60, line.capacity(), "two sign-ins a second while machines waited is 60 in 30 s")
}

func TestAQuietSpellLeavesTheLastPaceStanding(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	line := newSignInLine(1, 100, clock.read)
	serveAtPace(t, line, clock, 250*time.Millisecond)
	require.Equal(t, 120, line.capacity())

	clock.advance(time.Hour)
	for range 5 {
		quick, err := line.enter(context.Background(), fabrikamCustomer)
		require.NoError(t, err)
		quick()
	}

	assert.Equal(t, 120, line.capacity(), "sign-ins that never waited say nothing of the pace")
}

func TestTheLineScalesItsSignInsInProgressWithTheProcessor(t *testing.T) {
	t.Parallel()

	quarter, _ := signInLineSize(0.25)
	whole, _ := signInLineSize(1)
	sliver, _ := signInLineSize(0.0001)

	assert.Equal(t, 2, quarter, "two at once at a quarter processor read fastest on the rehearsal")
	assert.Greater(t, whole, quarter)
	assert.Equal(t, 1, sliver, "any processor share runs one sign-in")
}

func TestAMachineTurnedAwayIsToldToComeBackWithinThirtySeconds(t *testing.T) {
	t.Parallel()

	seen := map[uint32]bool{}
	for range 2000 {
		wait := comeBackWait()
		require.LessOrEqual(t, wait, uint32(30))
		seen[wait] = true
	}
	assert.True(t, seen[0] && seen[30], "the wait runs from nought to thirty seconds")
	assert.Len(t, seen, 31, "every whole second is drawn")
}

// without returns the sign-ins other than gone.
func without(all []*queued, gone *queued) []*queued {
	kept := make([]*queued, 0, len(all))
	for _, q := range all {
		if q != gone {
			kept = append(kept, q)
		}
	}
	return kept
}
