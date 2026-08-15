package alerts

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// The readings that are not incidents on their own.
//
// A fleet event where no host individually breaches is visible precisely because
// several hosts see it at once — one machine reading a little slower than usual
// is noise. So a low-severity observation is stored holding no room until a
// second machine says the same thing inside the window, and only then does the
// finding exist.

// TestObservationsNeedCoOccurrenceToOpenARoom drives E17. Fabrikam's file server
// and eleven workstations each get a little slower after a switch firmware push,
// and not one of them crosses a threshold. A single host's sub-threshold reading
// is noise and must open nothing; the same reading on a second machine inside the
// window is the estate-wide event, and both readings belong to it.
func TestObservationsNeedCoOccurrenceToOpenARoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeOrganization, Window: 30 * time.Minute}
	observation := func(deviceID uuid.UUID, when time.Time) Alert {
		return e.variant(func(a *Alert) {
			a.RuleID = "latency-drift"
			a.Severity = SeverityInfo
			a.DeviceID = deviceID
			at(when)(a)
		})
	}

	lone := observation(e.device, e.now)
	e.recordUnder(t, lone, grouping, Stored)
	assert.Zero(t, e.rooms(t, "latency-drift", ScopeOrganization),
		"one host being slightly slow is not an incident")
	assert.Equal(t, 1, e.count(t, qRoomlessSeen, e.org), "the observation is still recorded")

	// The same machine again is still one machine.
	e.recordUnder(t, observation(e.device, e.now.Add(5*time.Minute)), grouping, Stored)
	assert.Zero(t, e.rooms(t, "latency-drift", ScopeOrganization),
		"repetition on one host is not co-occurrence")

	// A second machine inside the window is the estate-wide event.
	fleet := e.fleet(t, 1)
	second := observation(fleet[0], e.now.Add(10*time.Minute))
	e.recordUnder(t, second, grouping, Stored)

	room := e.roomFor(t, grouping, "latency-drift", e.org)
	assert.Equal(t, 2, room.DeviceCount)
	assert.Equal(t, 3, room.Occurrences, "the readings that were waiting belong to the room they opened")
	assert.Equal(t, SeverityInfo, room.Severity)
	assert.Zero(t, e.count(t, qRoomlessSeen, e.org))
	assert.Equal(t, room.ID.String(), e.roomOf(t, lone.ID),
		"the first observation is part of the finding, not a loose row before it")
}

// TestASingleMachinesObservationsNeverOpenARoom pins the device-scoped half of
// E17. Cross-device co-occurrence cannot happen inside a room that is about one
// machine, so an observation-only rule at device scope opens nothing however
// often it fires — an observation is worth recording beside an incident, never
// worth raising one.
func TestASingleMachinesObservationsNeverOpenARoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: time.Hour}

	for i := range 5 {
		when := e.now.Add(time.Duration(i) * time.Minute)
		e.recordUnder(t, e.variant(func(a *Alert) {
			a.RuleID = "latency-drift"
			a.Severity = SeverityInfo
			at(when)(a)
		}), grouping, Stored)
	}

	assert.Zero(t, e.rooms(t, "latency-drift", ScopeDevice))
	assert.Equal(t, 5, e.count(t, qRoomlessSeen, e.org))
}

// TestAnObservationJoinsARoomThatIsAlreadyOpen keeps the previous two cases from
// being read as "observations are ignored". Once something has raised the room,
// the quiet readings around it are exactly the context an investigation wants.
func TestAnObservationJoinsARoomThatIsAlreadyOpen(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: time.Hour}

	e.recordUnder(t, e.variant(nil), grouping, Stored)
	e.recordUnder(t, e.variant(func(a *Alert) {
		a.Severity = SeverityInfo
		shifted(time.Minute)(a)
		a.ObservedAt = a.ObservedAt.Add(time.Minute)
	}), grouping, Stored)

	room := e.roomFor(t, grouping, "disk-critical", e.device)
	assert.Equal(t, 2, room.Occurrences)
	assert.Equal(t, SeverityCritical, room.Severity, "a room is as bad as the worst thing in it")
}
