package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheStormRoomClosesItselfOnceTheHourIsQuiet(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	e.seedHourOfAlerts(t, DefaultOrganizationHourlyCeiling, e.now.Add(-10*time.Minute))
	e.record(t, e.alert(), CeilingSuppressed)
	storm := e.roomFor(t, Grouping{Scope: ScopeOrganization}, StormRuleID, e.org)

	e.sweepAt(t, e.now.Add(59*time.Minute), nil, 0)
	e.sweepAt(t, e.now.Add(time.Hour), nil, 1)

	e.assertStatus(t, storm.ID, StatusResolved, "the storm room closes once the hour is quiet")
}

func TestTheSweepReachesEveryTenant(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	windows := map[string]time.Duration{"disk-critical": time.Hour}

	mine := e.openRoomAt(t, StatusNew, e.now.Add(-2*time.Hour))
	other := e.neighbour(t, "Fabrikam")
	theirs := e.openRoomIn(t, other, StatusNew, e.now.Add(-2*time.Hour))

	e.sweepAt(t, e.now, windows, 2)

	e.assertStatus(t, mine, StatusResolved, "the sweep closes the logged-in tenant's room")
	statusOther, _, _ := e.outcomeIn(t, other, theirs)
	assert.Equal(t, string(StatusResolved), statusOther,
		"a tenant with nobody logged in still has its queue kept honest")
}

func TestAnUnreachableStoreIsReportedByEveryDoor(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room := e.openRoomAt(t, StatusNew, e.now)
	closed := e.openRoomAt(t, StatusResolved, e.now)

	require.NoError(t, e.store.Close())

	assert.Error(t, e.alerts.Transition(e.ctx, room, Change{To: StatusAcknowledged}),
		"a move nobody made must not read as one that succeeded")
	assert.Error(t, e.alerts.Reopen(e.ctx, closed, uuid.New()))
	_, err := e.alerts.ResolveStale(context.Background(), map[string]time.Duration{"disk-critical": time.Hour})
	assert.Error(t, err, "a sweep that could not run must not report a tidy queue")
}

func TestTheStormHoldIsTheStoresOwn(t *testing.T) {
	t.Parallel()

	rules, seconds := holds(map[string]time.Duration{
		StormRuleID:     time.Minute,
		"disk-critical": 15 * time.Minute,
		"never-set":     0,
	})

	require.Len(t, rules, 2)
	byRule := map[string]float64{}
	for i, id := range rules {
		byRule[id] = seconds[i]
	}
	assert.Equal(t, StormHold.Seconds(), byRule[StormRuleID], "the storm's hour is not the caller's to set")
	assert.InDelta(t, (15 * time.Minute).Seconds(), byRule["disk-critical"], 0)
	assert.NotContains(t, byRule, "never-set", "a rule declaring no window closes nothing")
}
