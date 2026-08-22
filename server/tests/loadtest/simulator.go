package main

import "time"

// A machine is not a burst of connections.
//
// It connects once and stays connected for days. It sends on a cadence. It
// occasionally loses its link and comes back carrying a backlog, sometimes
// before the server has noticed the first connection is gone. A harness that
// only opens connections and closes them measures the accept path, which is a
// small fraction of what the server spends its time on.
//
// What a machine does next, and when, is decided here as a state machine, so
// the behaviour can be stepped through without a network and a run is
// reproducible from an agent id and a seed. Dialling belongs to the thin part
// around this.

// ActionKind is one thing a simulated machine does.
type ActionKind string

const (
	// ActionConnect opens the transport.
	ActionConnect ActionKind = "connect"
	// ActionRegister completes enrollment.
	ActionRegister ActionKind = "register"
	// ActionHeartbeat is the periodic liveness message, which also marks the
	// boundary of the previous telemetry burst.
	ActionHeartbeat ActionKind = "heartbeat"
	// ActionTelemetry emits one full telemetry cycle.
	ActionTelemetry ActionKind = "telemetry"
	// ActionBackfill drains one batch of the backlog a reconnect brought.
	ActionBackfill ActionKind = "backfill"
	// ActionDisconnect drops the transport the way a lost link does.
	ActionDisconnect ActionKind = "disconnect"
	// ActionDuplicateConnect opens a second connection for the same machine
	// while the first is still registered.
	ActionDuplicateConnect ActionKind = "duplicate-connect"
	// ActionStop ends this machine's participation deliberately, so a closed
	// connection is not counted as one the server dropped.
	ActionStop ActionKind = "stop"
)

// Action is what a machine does and when it does it.
type Action struct {
	Kind ActionKind
	At   time.Time
}

// Behaviour is one machine's whole arc, declared up front.
type Behaviour struct {
	// HeartbeatEvery and TelemetryEvery are the cadences. Zero disables that
	// message rather than sending it continuously.
	HeartbeatEvery time.Duration
	TelemetryEvery time.Duration

	// JitterFraction spreads the cadence across the fleet. Every machine firing
	// on the same instant is a shape no estate has, and it is the one that makes
	// a server look fine right up until it does not.
	JitterFraction float64

	// HoldFor is how long the machine stays in the run. Zero means until the
	// run stops it.
	HoldFor time.Duration

	// ReconnectAfter drops and re-establishes the connection on this interval.
	ReconnectAfter time.Duration
	// BackfillBatches is how many batches of backlog a reconnect carries.
	BackfillBatches int

	// DuplicateConnection opens a second connection for the same machine while
	// the first is still registered, so which one the server keeps is exercised
	// rather than assumed.
	DuplicateConnection bool

	// Tombstoned marks a machine whose device was purged. It keeps sending,
	// which is what proves the server refuses it — a simulator that politely
	// stopped would prove nothing.
	Tombstoned bool

	// ResponseDelay is how long this machine takes to answer a request, which
	// is what a congested link looks like from the server's side.
	ResponseDelay time.Duration
}

// simPhase is where a machine is in its own arc.
type simPhase int

const (
	phaseFresh simPhase = iota
	phaseConnected
	phaseRegistered
	phaseBackfilling
	phaseStopped
)

// SimAgent is one machine's state machine.
type SimAgent struct {
	id        uint64
	behaviour Behaviour
	startedAt time.Time

	phase          simPhase
	nextHeartbeat  time.Time
	nextTelemetry  time.Time
	nextReconnect  time.Time
	backfillLeft   int
	duplicateOwed  bool
	heartbeatShift time.Duration
	telemetryShift time.Duration
}

// NewSimAgent builds one machine. The jitter is derived from the id, so the
// same fleet spreads the same way on every run.
func NewSimAgent(id uint64, behaviour Behaviour, startedAt time.Time) *SimAgent {
	source := &sequence{state: id}
	return &SimAgent{
		id:             id,
		behaviour:      behaviour,
		startedAt:      startedAt,
		phase:          phaseFresh,
		duplicateOwed:  behaviour.DuplicateConnection,
		heartbeatShift: jitter(behaviour.HeartbeatEvery, behaviour.JitterFraction, source),
		telemetryShift: jitter(behaviour.TelemetryEvery, behaviour.JitterFraction, source),
	}
}

// ExpectsRejection reports whether everything this machine writes is supposed
// to be refused. Those refusals are the system working, so they are counted
// apart from faults.
func (a *SimAgent) ExpectsRejection() bool { return a.behaviour.Tombstoned }

// ResponseDelay is how long this machine takes to answer.
func (a *SimAgent) ResponseDelay() time.Duration { return a.behaviour.ResponseDelay }

// Done reports whether this machine has left the run.
func (a *SimAgent) Done(now time.Time) bool {
	return a.phase == phaseStopped || a.holdExpired(now)
}

func (a *SimAgent) holdExpired(now time.Time) bool {
	if a.behaviour.HoldFor <= 0 {
		return false
	}
	return !now.Before(a.startedAt.Add(a.behaviour.HoldFor))
}

// Next reports what this machine does next and when. It never mutates: the
// caller applies the action with Did, which is what lets a run decide not to.
func (a *SimAgent) Next(now time.Time) Action {
	if a.phase == phaseStopped {
		return Action{Kind: ActionStop, At: now}
	}
	if a.holdExpired(now) {
		return Action{Kind: ActionStop, At: now}
	}

	switch a.phase {
	case phaseFresh:
		return Action{Kind: ActionConnect, At: now}
	case phaseConnected:
		return Action{Kind: ActionRegister, At: now}
	case phaseBackfilling:
		// The backlog drains before the cadence resumes: a machine that just
		// came back has a queue, and the server admitting that queue is the
		// behaviour under test.
		return Action{Kind: ActionBackfill, At: now}
	case phaseRegistered, phaseStopped:
	}

	if a.duplicateOwed {
		return Action{Kind: ActionDuplicateConnect, At: now}
	}
	if !a.nextReconnect.IsZero() && !now.Before(a.nextReconnect) {
		return Action{Kind: ActionDisconnect, At: a.nextReconnect}
	}

	return a.nextScheduled(now)
}

// nextScheduled picks whichever cadence comes first.
func (a *SimAgent) nextScheduled(now time.Time) Action {
	next := Action{Kind: ActionStop, At: now}
	if !a.nextHeartbeat.IsZero() {
		next = Action{Kind: ActionHeartbeat, At: a.nextHeartbeat}
	}
	if !a.nextTelemetry.IsZero() && (next.Kind == ActionStop || a.nextTelemetry.Before(next.At)) {
		next = Action{Kind: ActionTelemetry, At: a.nextTelemetry}
	}
	if next.Kind == ActionStop {
		// Nothing on a cadence and nothing owed: the machine holds its
		// connection open, which is itself the load a fleet applies.
		return Action{Kind: ActionStop, At: now}
	}
	return next
}

// Did records that an action happened, advancing the machine.
func (a *SimAgent) Did(kind ActionKind, at time.Time) {
	switch kind {
	case ActionConnect:
		a.phase = phaseConnected
	case ActionRegister:
		a.onRegistered(at)
	case ActionHeartbeat:
		a.nextHeartbeat = at.Add(a.behaviour.HeartbeatEvery)
	case ActionTelemetry:
		a.nextTelemetry = at.Add(a.behaviour.TelemetryEvery)
	case ActionDuplicateConnect:
		a.duplicateOwed = false
	case ActionDisconnect:
		a.onDisconnected(at)
	case ActionBackfill:
		a.onBackfilled()
	case ActionStop:
		a.phase = phaseStopped
	}
}

func (a *SimAgent) onRegistered(at time.Time) {
	a.phase = phaseRegistered
	// A machine that just came back has a queue, and how the server admits that
	// queue is one of the things a load run exists to test — so the backlog is
	// drained before the cadence resumes.
	if a.backfillLeft > 0 {
		a.phase = phaseBackfilling
	}
	// The first message of each cadence is one interval away rather than
	// immediate: a machine that registers does not also report in the same
	// instant, and counting that would put a spike at the start of every run.
	if a.behaviour.HeartbeatEvery > 0 {
		a.nextHeartbeat = at.Add(a.behaviour.HeartbeatEvery + a.heartbeatShift)
	}
	if a.behaviour.TelemetryEvery > 0 {
		a.nextTelemetry = at.Add(a.behaviour.TelemetryEvery + a.telemetryShift)
	}
	if a.behaviour.ReconnectAfter > 0 {
		a.nextReconnect = at.Add(a.behaviour.ReconnectAfter)
	}
}

func (a *SimAgent) onDisconnected(at time.Time) {
	a.phase = phaseFresh
	a.backfillLeft = a.behaviour.BackfillBatches
	a.nextHeartbeat = time.Time{}
	a.nextTelemetry = time.Time{}
	if a.behaviour.ReconnectAfter > 0 {
		a.nextReconnect = at.Add(a.behaviour.ReconnectAfter)
	}
}

func (a *SimAgent) onBackfilled() {
	a.backfillLeft--
	if a.backfillLeft <= 0 {
		a.phase = phaseRegistered
	}
}

// jitter spreads one cadence deterministically across ±fraction of itself.
func jitter(interval time.Duration, fraction float64, source *sequence) time.Duration {
	if interval <= 0 || fraction <= 0 {
		return 0
	}
	span := int(float64(interval) * fraction)
	if span <= 0 {
		return 0
	}
	return time.Duration(source.below(2*span) - span)
}

// Ramp is how a fleet arrives. A step change and a ramp are different events
// that the system absorbs differently, so both are expressible: a zero ramp
// means everything at once, which is a site whose link came back, and a
// non-zero one means arrival spread over that window.
type Ramp struct {
	Total int
	Over  time.Duration
}

// ConnectedAt is how many machines are connected once elapsed has passed. It
// never overshoots the fleet and never goes backwards.
func (r Ramp) ConnectedAt(elapsed time.Duration) int {
	if r.Total <= 0 {
		return 0
	}
	if r.Over <= 0 || elapsed >= r.Over {
		return r.Total
	}
	if elapsed <= 0 {
		return 0
	}
	connected := int(float64(r.Total) * (float64(elapsed) / float64(r.Over)))
	if connected > r.Total {
		return r.Total
	}
	return connected
}
