package alerts

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

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
		e.recordedAt(t, grouping, when, func(a *Alert) { a.DeviceID = fleet[i%machines] })
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

func TestRecurrenceFoldsAcrossTimeAndTheWindowIsWhatDoesIt(t *testing.T) {
	t.Parallel()

	daily := func(t *testing.T, e estate, window time.Duration) Grouping {
		t.Helper()
		grouping := Grouping{Scope: ScopeDevice, Window: window}
		for day := range 30 {
			when := e.now.Add(-time.Duration(30-day) * 24 * time.Hour)
			e.recordedAt(t, grouping, when, func(a *Alert) { a.RuleID = "workstation-freeze" })
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

func TestSlowBurnFoldsUntilTheWindowLapses(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: 24 * time.Hour}

	start := e.now.Add(-7 * 24 * time.Hour)
	for day := range 7 {
		e.recordedAt(t, grouping, start.Add(time.Duration(day)*24*time.Hour), nil)
	}

	assert.Equal(t, 1, e.rooms(t, "disk-critical", ScopeDevice),
		"exactly a window apart is still the same problem")
	first := e.roomFor(t, grouping, "disk-critical", e.device)
	assert.Equal(t, 7, first.Occurrences)

	lapsed := start.Add(7 * 24 * time.Hour).Add(time.Hour)
	e.recordedAt(t, grouping, lapsed, nil)

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

func TestScopeKeyIsDerivedFromTheMachinesOwnLadder(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	site := testutil.SeedSiteIn(t, e.ctx, e.store, e.org)
	housemate := testutil.SeedDevice(t, e.ctx, e.store, site.ID)
	elsewhere := testutil.SeedSiteIn(t, e.ctx, e.store, e.org)
	stranger := testutil.SeedDevice(t, e.ctx, e.store, elsewhere.ID)

	grouping := Grouping{Scope: ScopeSite, Window: time.Hour}
	for i, id := range []uuid.UUID{housemate.ID, stranger.ID} {
		e.recordedAt(t, grouping, e.now.Add(time.Duration(i)*time.Minute), func(a *Alert) { a.DeviceID = id })
	}

	assert.Equal(t, 2, e.rooms(t, "disk-critical", ScopeSite),
		"two offices with the same problem are two rooms with two people to call")
	assert.Equal(t, 1, e.roomFor(t, grouping, "disk-critical", site.ID).Occurrences)
	assert.Equal(t, 1, e.roomFor(t, grouping, "disk-critical", elsewhere.ID).Occurrences)
}

func (e estate) recordedAt(t *testing.T, g Grouping, when time.Time, change func(*Alert)) {
	t.Helper()
	e.recordUnder(t, e.variant(func(a *Alert) {
		if change != nil {
			change(a)
		}
		at(when)(a)
	}), g, Stored)
}

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
