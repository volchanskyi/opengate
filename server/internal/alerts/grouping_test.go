package alerts

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// The three fixtures the two-axis design exists for, and where a room's key
// comes from.
//
// Grouping is the whole value of an incident: 312 alerts a technician cannot
// read are one room they can. How wide a room is, is the axis everybody
// implements; how long firings stay one room is the axis that carries WS-4471,
// where no single freeze is worth a callout and the pattern is the diagnosis.
// Every fixture asserts the count of rooms, the count of machines and the count
// of alerts together, because conflating the last two is the mistake that
// survives a passing test.

// TestContosoRolloutFoldsIntoOneRoom drives C2. A bad driver reaches forty of
// Contoso's machines at 02:41 and each raises alerts for half an hour. Three
// hundred and twelve rows is not something a person on call reads; one room
// saying forty machines is.
//
// The two counts are asserted together deliberately. They differ by an order of
// magnitude here, which is exactly the fixture that catches an implementation
// counting alerts where it meant machines — a version that conflates them passes
// every fixture where one device raises one alert.
func TestContosoRolloutFoldsIntoOneRoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	const (
		machines = 40
		alerts   = 312
		spread   = 28 * time.Minute
	)
	fleet := e.fleet(t, machines)
	grouping := Grouping{Scope: ScopeOrganization, Window: 30 * time.Minute}

	for i := range alerts {
		when := e.now.Add(time.Duration(i) * spread / alerts)
		e.recordUnder(t, e.variant(func(a *Alert) {
			a.DeviceID = fleet[i%machines]
			at(when)(a)
		}), grouping, Stored)
	}

	assert.Equal(t, 1, e.rooms(t, "disk-critical", ScopeOrganization),
		"one estate-wide event is one room, whatever it costs in alerts")
	room := e.roomFor(t, grouping, "disk-critical", e.org)
	assert.Equal(t, machines, room.DeviceCount, "device_count is machines, not alerts")
	assert.Equal(t, alerts, room.Occurrences, "occurrences is alerts, not machines")
	assert.Equal(t, StatusNew, room.Status, "an unclaimed room is the triage queue")
	assert.Equal(t, SeverityCritical, room.Severity)
	assert.Equal(t, e.now, room.FirstSeen.UTC(), "the room starts when the estate did")
}

// TestRecurrenceFoldsAcrossTimeAndTheWindowIsWhatDoesIt drives C3, the row the
// whole two-axis design exists for. WS-4471 freezes once a day for a month. No
// single freeze is worth a callout and each looks like a one-off; the pattern is
// the diagnosis, and only grouping across time makes it one.
//
// The control case is the point of the test. Thirty alerts folding into one room
// proves nothing on its own — an implementation that groups on the key alone and
// ignores the window passes it. Running the same thirty alerts under half an hour
// and getting thirty rooms is what shows the window is load-bearing.
func TestRecurrenceFoldsAcrossTimeAndTheWindowIsWhatDoesIt(t *testing.T) {
	t.Parallel()

	daily := func(t *testing.T, e estate, window time.Duration) Grouping {
		t.Helper()
		grouping := Grouping{Scope: ScopeDevice, Window: window}
		for day := range 30 {
			when := e.now.Add(-time.Duration(30-day) * 24 * time.Hour)
			e.recordUnder(t, e.variant(func(a *Alert) {
				a.RuleID = "workstation-freeze"
				at(when)(a)
			}), grouping, Stored)
		}
		return grouping
	}

	t.Run("a week-long window makes thirty freezes one diagnosis", func(t *testing.T) {
		t.Parallel()
		e := newEstate(t)
		grouping := daily(t, e, 7*24*time.Hour)

		assert.Equal(t, 1, e.rooms(t, "workstation-freeze", ScopeDevice))
		room := e.roomFor(t, grouping, "workstation-freeze", e.device)
		assert.Equal(t, 30, room.Occurrences, "thirty occurrences is the finding")
		assert.Equal(t, 1, room.DeviceCount, "one machine froze thirty times")
	})

	t.Run("half an hour fragments the same thirty into thirty", func(t *testing.T) {
		t.Parallel()
		e := newEstate(t)
		daily(t, e, 30*time.Minute)

		assert.Equal(t, 30, e.rooms(t, "workstation-freeze", ScopeDevice),
			"the window, not the key, is what folds a recurrence")
	})
}

// TestSlowBurnFoldsUntilTheWindowLapses drives C4 on the boundary rather than
// near it. FS01's disk fills over a week and re-fires each day; a day apart is
// the same problem still happening, and the assertion that matters is which side
// of the declared window each gap lands on.
func TestSlowBurnFoldsUntilTheWindowLapses(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: 24 * time.Hour}

	start := e.now.Add(-7 * 24 * time.Hour)
	for day := range 7 {
		when := start.Add(time.Duration(day) * 24 * time.Hour)
		e.recordUnder(t, e.variant(func(a *Alert) {
			at(when)(a)
		}), grouping, Stored)
	}

	assert.Equal(t, 1, e.rooms(t, "disk-critical", ScopeDevice),
		"exactly a window apart is still the same problem")
	first := e.roomFor(t, grouping, "disk-critical", e.device)
	assert.Equal(t, 7, first.Occurrences)

	// An hour past the window is a different episode, and the room it left
	// behind is closed rather than left in the queue with nothing arriving.
	lapsed := start.Add(7 * 24 * time.Hour).Add(time.Hour)
	e.recordUnder(t, e.variant(func(a *Alert) {
		at(lapsed)(a)
	}), grouping, Stored)

	assert.Equal(t, 2, e.rooms(t, "disk-critical", ScopeDevice))
	second := e.roomFor(t, grouping, "disk-critical", e.device)
	assert.NotEqual(t, first.ID, second.ID, "the lapsed room does not take the new episode")
	assert.Equal(t, 1, second.Occurrences)

	status, _, resolvedAt := e.outcome(t, first.ID)
	assert.Equal(t, string(StatusResolved), status,
		"a room nothing can still fold into is closed, not left open forever")
	require.True(t, resolvedAt.Valid)
	assert.Equal(t, first.LastSeen.UTC().Add(24*time.Hour), resolvedAt.Time.UTC(),
		"it closed when it became closeable, not when the next alert happened to arrive")
}

// TestScopeKeyIsDerivedFromTheMachinesOwnLadder pins where a grouping key comes
// from. It is read from the machine's place in the tenancy ladder on this side,
// never taken from the endpoint: a device that could name its own key could name
// another customer's room and file its alerts into it.
func TestScopeKeyIsDerivedFromTheMachinesOwnLadder(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	site := testutil.SeedSiteIn(t, e.ctx, e.store, e.org)
	housemate := testutil.SeedDevice(t, e.ctx, e.store, site.ID)
	elsewhere := testutil.SeedSiteIn(t, e.ctx, e.store, e.org)
	stranger := testutil.SeedDevice(t, e.ctx, e.store, elsewhere.ID)

	grouping := Grouping{Scope: ScopeSite, Window: time.Hour}
	for i, id := range []uuid.UUID{housemate.ID, stranger.ID} {
		when := e.now.Add(time.Duration(i) * time.Minute)
		e.recordUnder(t, e.variant(func(a *Alert) {
			a.DeviceID = id
			at(when)(a)
		}), grouping, Stored)
	}

	assert.Equal(t, 2, e.rooms(t, "disk-critical", ScopeSite),
		"two offices with the same problem are two rooms with two people to call")
	assert.Equal(t, 1, e.roomFor(t, grouping, "disk-critical", site.ID).Occurrences)
	assert.Equal(t, 1, e.roomFor(t, grouping, "disk-critical", elsewhere.ID).Occurrences)
}

// TestAnUnfiledMachineGetsARoomOfItsOwn is the site-scoped edge. A machine that
// belongs to no office cannot be grouped with one, and pooling every unfiled
// machine under one absent key would put unrelated estates in a room with no
// correct assignee. The narrowest thing that can honestly be named is the
// machine itself.
func TestAnUnfiledMachineGetsARoomOfItsOwn(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	e.exec(t, `UPDATE devices SET site_id = NULL WHERE id = $1`, e.device)

	grouping := Grouping{Scope: ScopeSite, Window: time.Hour}
	e.recordUnder(t, e.variant(nil), grouping, Stored)

	assert.Zero(t, e.rooms(t, "disk-critical", ScopeSite))
	assert.Equal(t, 1, e.rooms(t, "disk-critical", ScopeDevice),
		"an unfiled machine is its own room rather than everyone's")
}
