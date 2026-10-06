package alerts

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aRoomHeldBy opens a room from one alert and returns it with the hold its grouping window sets.
func (e estate) aRoomHeldBy(t *testing.T, g Grouping) (Incident, map[string]time.Duration) {
	t.Helper()
	e.recordUnder(t, e.variant(nil), g, Stored)
	return e.roomFor(t, g, "disk-critical", e.scopeKeyFor(g)), map[string]time.Duration{"disk-critical": g.Window}
}

func (e estate) scopeKeyFor(g Grouping) uuid.UUID {
	if g.Scope == ScopeOrganization {
		return e.org
	}
	return e.device
}

func (e estate) assertStatus(t *testing.T, id uuid.UUID, want Status, msg string) {
	t.Helper()
	status, _, _ := e.outcome(t, id)
	assert.Equal(t, string(want), status, msg)
}

func TestAutoResolveWaitsOutTheWholeReopenWindow(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room, windows := e.aRoomHeldBy(t, Grouping{Scope: ScopeDevice, Window: 24 * time.Hour})

	e.sweepAt(t, room.LastSeen.Add(24*time.Hour).Add(-time.Second), windows, 0)
	e.assertStatus(t, room.ID, StatusNew, "a second short of the window is still inside it")

	e.sweepAt(t, room.LastSeen.Add(24*time.Hour), windows, 1)
	status, cause, resolvedAt := e.outcome(t, room.ID)
	assert.Equal(t, string(StatusResolved), status)
	assert.False(t, cause.Valid,
		"a cause code is a person's answer, and the system must not put words in their mouth")
	require.True(t, resolvedAt.Valid)
	assert.Equal(t, room.LastSeen.UTC().Add(24*time.Hour), resolvedAt.Time.UTC(),
		"it closed when it became closeable, not when the sweep happened to run")

	history := e.history(t, room.ID)
	require.Len(t, history, 1)
	assert.Equal(t, "resolution", history[0].kind)
	assert.Empty(t, history[0].actor, "nobody did this, so nobody is named")

	e.sweepAt(t, room.LastSeen.Add(72*time.Hour), windows, 0)
}

func TestOneAlertInsideTheWindowResetsTheClock(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: 24 * time.Hour}
	room, windows := e.aRoomHeldBy(t, grouping)

	halfway := room.LastSeen.Add(12 * time.Hour)
	e.recordUnder(t, e.variant(at(halfway)), grouping, Stored)

	e.sweepAt(t, room.LastSeen.Add(24*time.Hour), windows, 0)
	e.assertStatus(t, room.ID, StatusNew, "the hold runs from the last occurrence")

	e.sweepAt(t, halfway.Add(24*time.Hour), windows, 1)
}

func TestAMachineToldToGoQuietKeepsItsRoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room, windows := e.aRoomHeldBy(t, Grouping{Scope: ScopeDevice, Window: 24 * time.Hour})
	e.exec(t, `UPDATE devices SET maintenance_on = TRUE WHERE id = $1`, e.device)

	e.sweepAt(t, room.LastSeen.Add(72*time.Hour), windows, 0)
	e.assertStatus(t, room.ID, StatusNew, "an incident open before maintenance does not auto-resolve during it")

	e.exec(t, `UPDATE devices SET maintenance_on = FALSE WHERE id = $1`, e.device)
	e.sweepAt(t, room.LastSeen.Add(72*time.Hour), windows, 1)
}

func TestMaintenanceShieldsOnlyTheRoomThatIsAboutThatMachine(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room, windows := e.aRoomHeldBy(t, Grouping{Scope: ScopeOrganization, Window: 30 * time.Minute})
	e.exec(t, `UPDATE devices SET maintenance_on = TRUE WHERE id = $1`, e.device)

	e.sweepAt(t, room.LastSeen.Add(time.Hour), windows, 1)
}

func TestASweepLeavesARoomItHasNoWindowFor(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room := e.openRoomAt(t, StatusNew, e.now.Add(-365*24*time.Hour))

	e.sweepAt(t, e.now, map[string]time.Duration{"some-other-rule": time.Minute}, 0)
	e.assertStatus(t, room, StatusNew, "a room with no window for its rule stays open")
}
