package alerts

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

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

	assert.ErrorIs(t, e.alerts.Reopen(e.ctx, room, tech), ErrIllegalTransition)
}

func TestReopeningYieldsToTheRoomThatTookItsPlace(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	tech := testutil.SeedUser(t, e.ctx, e.store).ID
	grouping := Grouping{Scope: ScopeDevice, Window: time.Hour}

	e.recordUnder(t, e.variant(nil), grouping, Stored)
	first := e.roomFor(t, grouping, "disk-critical", e.device).ID
	require.NoError(t, e.alerts.Transition(e.ctx, first,
		Change{To: StatusResolved, Cause: CauseResolvedSelf, Actor: tech}))

	// A day later the same condition opens a new room; the closed one is outside the fold's index.
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
