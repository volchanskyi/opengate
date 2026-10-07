package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type remaining struct {
	alerts, incidents, events int
}

type sweepCase struct {
	name    string
	arrange func(t *testing.T, e estate)
	sweeps  int
	removed int
	left    remaining
}

func TestSweepExpired(t *testing.T) {
	t.Parallel()

	for _, tc := range []sweepCase{{
		name:    "an alert inside the horizon is kept",
		arrange: func(t *testing.T, e estate) { e.aged(t, e.now.Add(-year+time.Hour), nil) },
		left:    remaining{alerts: 1, incidents: 1},
	}, {
		name:    "an alert past the horizon goes",
		arrange: func(t *testing.T, e estate) { e.aged(t, e.now.Add(-year-time.Hour), nil) },
		removed: 1,
		left:    remaining{incidents: 1},
	}, {
		name: "age is counted from receipt, not from the event",
		// A retroactive finding: it happened two years ago and arrived today.
		// The horizon is about how long the row has been held, so it survives.
		arrange: func(t *testing.T, e estate) { e.aged(t, e.now, at(e.now.Add(-2*year))) },
		left:    remaining{alerts: 1, incidents: 1},
	}, {
		name: "a closed room still holding an alert is kept",
		// Removing the room would leave that alert pointing at nothing, which
		// reads as a finding nobody ever investigated. So neither goes.
		arrange: func(t *testing.T, e estate) {
			e.aged(t, e.now.Add(-time.Hour), nil)
			e.resolveRoomAt(t, e.roomHolding(t), e.now.Add(-year-time.Hour))
		},
		left: remaining{alerts: 1, incidents: 1, events: 1},
	}, {
		name: "a closed room nothing points at goes, and takes its history",
		arrange: func(t *testing.T, e estate) {
			e.aged(t, e.now.Add(-year-time.Hour), nil)
			e.resolveRoomAt(t, e.roomHolding(t), e.now.Add(-year-time.Hour))
		},
		removed: 2,
	}, {
		name: "an open room is kept however old",
		// It is somebody's outstanding work, and age alone never closes one.
		arrange: func(t *testing.T, e estate) { e.aged(t, e.now.Add(-year-time.Hour), nil) },
		removed: 1,
		left:    remaining{incidents: 1},
	}, {
		name: "a second pass over a swept store reclaims nothing",
		arrange: func(t *testing.T, e estate) {
			e.aged(t, e.now.Add(-year-time.Hour), nil)
			e.resolveRoomAt(t, e.roomHolding(t), e.now.Add(-year-time.Hour))
		},
		sweeps:  2,
		removed: 2,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEstate(t)
			tc.arrange(t, e)

			for pass := range max(tc.sweeps, 1) {
				want := 0
				if pass == 0 {
					want = tc.removed
				}
				e.expireAt(t, e.now, year, want)
			}

			assert.Equal(t, tc.left.alerts, e.countIn(t, "alerts"), "alerts left")
			assert.Equal(t, tc.left.incidents, e.countIn(t, "incidents"), "incidents left")
			assert.Equal(t, tc.left.events, e.countIn(t, "incident_events"), "history left")
		})
	}
}

func TestSweepExpiredKeepsTheAlertThatIsStillInsideTheHorizon(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	old := e.aged(t, e.now.Add(-year-time.Hour), nil)
	kept := e.aged(t, e.now.Add(-time.Hour), shifted(time.Hour))
	require.NotEqual(t, old, kept)

	e.expireAt(t, e.now, year, 1)

	var surviving uuid.UUID
	e.readOne(t, `SELECT id FROM alerts`, nil, &surviving)
	assert.Equal(t, kept, surviving)
}

func TestSweepExpiredReachesEveryTenant(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	e.aged(t, e.now.Add(-year-time.Hour), nil)
	other := e.neighbour(t, "Fabrikam")
	theirs := e.openRoomIn(t, other, StatusResolved, e.now.Add(-year-time.Hour))

	e.expireAt(t, e.now, year, 2)

	assert.Zero(t, e.countIn(t, "alerts"))
	assert.Zero(t, e.roomsIn(t, other, theirs),
		"a tenant with nobody logged in still has its records aged out")
}

func TestSweepExpiredRefuses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		horizon time.Duration
		ctx     func() context.Context
		wantErr error
	}{
		{"a zero horizon", 0, context.Background, ErrHorizonNotPositive},
		{"a negative horizon", -time.Hour, context.Background, ErrHorizonNotPositive},
		{"a cancelled context", year, cancelledContext, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEstate(t)
			e.aged(t, e.now.Add(-2*year), nil)

			e.alerts.now = func() time.Time { return e.now }
			removed, err := e.alerts.SweepExpired(tc.ctx(), tc.horizon)

			require.ErrorIs(t, err, tc.wantErr)
			assert.Zero(t, removed)
			assert.Equal(t, 1, e.countIn(t, "alerts"), "a refused sweep removes nothing")
		})
	}
}
