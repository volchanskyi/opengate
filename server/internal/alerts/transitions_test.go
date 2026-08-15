package alerts

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// How a room is allowed to move, and what each refusal means.
//
// A room in `new` *is* the triage queue, so these are the queue's rules. Its own
// columns say where it stands; the event rows say how it got there, which is
// what a handover between two technicians reads. A resolution needs a cause
// code, because `false_positive` is the only channel that says which curated
// rule needs its threshold moved — a resolve that skips it silently spends the
// feedback the rule pack is tuned from.

// TestEveryLegalTransitionSucceedsAndIsRecorded drives the first half of C5. The
// room's own columns say where it stands; the event rows say how it got there,
// which is what a handover between two technicians reads.
func TestEveryLegalTransitionSucceedsAndIsRecorded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		from  Status
		to    Status
		cause CauseCode
		kind  string
	}{
		{"picked up out of the queue", StatusNew, StatusAcknowledged, "", "status_change"},
		{"taken straight into the work", StatusNew, StatusInvestigating, "", "status_change"},
		{"closed off the queue as noise", StatusNew, StatusResolved, CauseFalsePositive, "resolution"},
		{"started on", StatusAcknowledged, StatusInvestigating, "", "status_change"},
		{"handed back to the queue", StatusAcknowledged, StatusNew, "", "status_change"},
		{"closed after a fix", StatusAcknowledged, StatusResolved, CauseFixedByTech, "resolution"},
		{"put down again", StatusInvestigating, StatusAcknowledged, "", "status_change"},
		{"handed back mid-shift", StatusInvestigating, StatusNew, "", "status_change"},
		{"finished", StatusInvestigating, StatusResolved, CauseHardwareFault, "resolution"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEstate(t)
			tech := testutil.SeedUser(t, e.ctx, e.store).ID
			room := e.openRoomAt(t, tc.from, e.now)

			require.NoError(t, e.alerts.Transition(e.ctx, room,
				Change{To: tc.to, Cause: tc.cause, Actor: tech}))

			status, cause, resolvedAt := e.outcome(t, room)
			assert.Equal(t, string(tc.to), status)
			assert.Equal(t, tc.to == StatusResolved, cause.Valid,
				"a cause code belongs to a resolution and to nothing else")
			assert.Equal(t, tc.to == StatusResolved, resolvedAt.Valid)

			history := e.history(t, room)
			require.Len(t, history, 1, "every transition leaves exactly one line of history")
			assert.Equal(t, tc.kind, history[0].kind)
			assert.Equal(t, tech.String(), history[0].actor, "who did it is half of a handover")
			assert.Contains(t, history[0].body, string(tc.from))
			assert.Contains(t, history[0].body, string(tc.to))
		})
	}
}

// TestIllegalTransitionsAreRefusedByName drives the second half of C5. Each of
// these is a different mistake with a different fix, so each is its own error
// rather than one rejection an API would have to guess the meaning of.
func TestIllegalTransitionsAreRefusedByName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		from   Status
		change Change
		want   error
	}{
		{
			name:   "a closed room is not walked back into",
			from:   StatusResolved,
			change: Change{To: StatusInvestigating},
			want:   ErrIllegalTransition,
		},
		{
			name:   "nor re-closed",
			from:   StatusResolved,
			change: Change{To: StatusResolved, Cause: CauseFixedByTech},
			want:   ErrIllegalTransition,
		},
		{
			name:   "standing still is not a move",
			from:   StatusAcknowledged,
			change: Change{To: StatusAcknowledged},
			want:   ErrIllegalTransition,
		},
		{
			name:   "closing without saying why spends the feedback channel",
			from:   StatusInvestigating,
			change: Change{To: StatusResolved},
			want:   ErrCauseRequired,
		},
		{
			name:   "a cause code on anything else is a mis-filled form",
			from:   StatusNew,
			change: Change{To: StatusAcknowledged, Cause: CauseResolvedSelf},
			want:   ErrCauseNotAllowed,
		},
		{
			name:   "a status nothing can render",
			from:   StatusNew,
			change: Change{To: Status("escalated")},
			want:   ErrUnknownStatus,
		},
		{
			name:   "a cause code nothing can report on",
			from:   StatusNew,
			change: Change{To: StatusResolved, Cause: CauseCode("someone-elses-problem")},
			want:   ErrUnknownCause,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEstate(t)
			room := e.openRoomAt(t, tc.from, e.now)

			err := e.alerts.Transition(e.ctx, room, tc.change)

			assert.ErrorIs(t, err, tc.want)
			status, _, _ := e.outcome(t, room)
			assert.Equal(t, string(tc.from), status, "a refused move changes nothing")
			assert.Empty(t, e.history(t, room), "and writes no history either")
		})
	}
}

// TestATransitionOnNothingIsRefused keeps a guessed id from reading as success,
// and a room in another tenant from being distinguishable from one that does not
// exist.
func TestATransitionOnNothingIsRefused(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	assert.ErrorIs(t, e.alerts.Transition(e.ctx, uuid.New(), Change{To: StatusAcknowledged}),
		ErrIncidentNotFound)

	room := e.openRoomAt(t, StatusNew, e.now)
	other := e.neighbour(t, "Fabrikam")
	assert.ErrorIs(t, e.alerts.Transition(other.ctx, room, Change{To: StatusAcknowledged}),
		ErrIncidentNotFound, "another tenant's room is indistinguishable from no room")
}

// TestReopeningIsItsOwnDoor is why `resolved -> investigating` is refused above.
// A technician who closed something that was not fixed has to be able to say so,
// but it is a different act from carrying on with an open room: it undoes an
// answer that has already been given, so it clears the cause code rather than
// leaving a closed room's reason attached to an open one.
func TestReopeningIsItsOwnDoor(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	tech := testutil.SeedUser(t, e.ctx, e.store).ID
	room := e.openRoomAt(t, StatusNew, e.now)
	require.NoError(t, e.alerts.Transition(e.ctx, room,
		Change{To: StatusResolved, Cause: CauseResolvedSelf, Actor: tech}))

	require.NoError(t, e.alerts.Reopen(e.ctx, room, tech))

	status, cause, resolvedAt := e.outcome(t, room)
	assert.Equal(t, string(StatusInvestigating), status)
	assert.False(t, cause.Valid, "the answer that turned out to be wrong is withdrawn with it")
	assert.False(t, resolvedAt.Valid)

	history := e.history(t, room)
	require.Len(t, history, 2)
	assert.Equal(t, "status_change", history[1].kind)
	assert.Contains(t, history[1].body, "reopened")

	// Reopening one that is already open is not a second act.
	assert.ErrorIs(t, e.alerts.Reopen(e.ctx, room, tech), ErrIllegalTransition)
}

// TestReopeningYieldsToTheRoomThatTookItsPlace is the collision the partial
// unique index would otherwise refuse with a constraint error nobody can act on.
// Once the same condition has recurred and opened a fresh room, the closed one
// cannot come back — there is exactly one open room per key, and the live one is
// where the alerts are landing.
func TestReopeningYieldsToTheRoomThatTookItsPlace(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	tech := testutil.SeedUser(t, e.ctx, e.store).ID
	grouping := Grouping{Scope: ScopeDevice, Window: time.Hour}

	e.recordUnder(t, e.variant(nil), grouping, Stored)
	first := e.roomFor(t, grouping, "disk-critical", e.device).ID
	require.NoError(t, e.alerts.Transition(e.ctx, first,
		Change{To: StatusResolved, Cause: CauseResolvedSelf, Actor: tech}))

	// The same condition, a day later: a new room, because the closed one is
	// outside the index the fold keys on.
	later := e.now.Add(24 * time.Hour)
	e.recordUnder(t, e.variant(func(a *Alert) {
		at(later)(a)
	}), grouping, Stored)
	second := e.roomFor(t, grouping, "disk-critical", e.device).ID
	require.NotEqual(t, first, second)

	assert.ErrorIs(t, e.alerts.Reopen(e.ctx, first, tech), ErrKeyAlreadyOpen)
	status, _, _ := e.outcome(t, first)
	assert.Equal(t, string(StatusResolved), status)
}
