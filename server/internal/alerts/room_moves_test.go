package alerts

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// What a person can do to a room, and the one thing they can ask it for
// afterwards.
//
// Assigning and commenting are the two moves that are not lifecycle: neither
// changes where an incident stands, and both are how two technicians hand work
// between them. Each lands in the same append-only history a status change
// does, because a room's own columns say where it stands now and only the
// history says how it got there.
//
// Evidence is the third thing, and it is a read rather than a move — but it
// belongs here because it is reachable only through the room, which is what
// keeps a guessable alert id from naming somebody else's incident.

// TestAssignHandsARoomOverAndSaysSo. Who is working an incident is a fact two
// technicians coordinate on, so it is both a column — what the queue filters
// on — and a line in the timeline, which is what says when it changed hands.
func TestAssignHandsARoomOverAndSaysSo(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	first := testutil.SeedUser(t, e.ctx, e.store)
	second := testutil.SeedUser(t, e.ctx, e.store)
	id := e.openRoomAt(t, StatusNew, e.now)

	e.clockAt(e.now.Add(time.Minute))
	require.NoError(t, e.alerts.Assign(e.ctx, id, first.ID, first.ID))
	assert.Equal(t, first.ID, e.opened(t, id).Incident.AssigneeID)

	e.clockAt(e.now.Add(2 * time.Minute))
	require.NoError(t, e.alerts.Assign(e.ctx, id, second.ID, first.ID))
	e.clockAt(e.now.Add(3 * time.Minute))
	require.NoError(t, e.alerts.Assign(e.ctx, id, uuid.Nil, second.ID))

	room := e.opened(t, id)
	assert.Equal(t, uuid.Nil, room.Incident.AssigneeID, "a room can be put back down")
	assert.Equal(t, []string{"assignment", "assignment", "assignment"}, kinds(room))
	assert.Contains(t, string(room.Events[2].Body), "unassigned",
		"the timeline says a room was put down, not that somebody took it")
}

// TestCommentBecomesALineInTheTimeline. A comment is not a field on the
// incident — it is one more thing that happened, in the order it happened, which
// is why it is the same append-only history a status change lands in.
func TestCommentBecomesALineInTheTimeline(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)
	id := e.openRoomAt(t, StatusNew, e.now)

	event, err := e.alerts.Comment(e.ctx, id, user.ID, "array controller replaced, watching overnight")
	require.NoError(t, err)
	assert.Equal(t, "comment", event.Kind)
	assert.Equal(t, user.ID, event.ActorID)

	room := e.opened(t, id)
	require.Len(t, room.Events, 1)
	assert.Contains(t, string(room.Events[0].Body), "array controller replaced")
}

// TestCommentRefusesWhatIsNotOne. An empty comment is a line in a handover that
// says nothing, and an unbounded one is a person's text box deciding how much a
// row weighs.
func TestCommentRefusesWhatIsNotOne(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)
	id := e.openRoomAt(t, StatusNew, e.now)

	for _, tc := range []struct {
		name string
		body string
	}{
		{"nothing at all", ""},
		{"only whitespace", "   \n\t "},
		{"more than a comment", strings.Repeat("x", maxCommentBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.alerts.Comment(e.ctx, id, user.ID, tc.body)
			assert.ErrorIs(t, err, ErrCommentUnusable)
		})
	}

	assert.Empty(t, e.opened(t, id).Events, "a refused comment leaves no line behind")
}

// TestEvidenceIsReadBackWholeAndByItsRoom. Evidence is frozen at write time and
// there is no path for asking the machine again, so what comes back is what
// arrived — and it comes back only through the room it belongs to, so an alert
// id on its own names nothing.
func TestEvidenceIsReadBackWholeAndByItsRoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	alert := e.variant(nil)
	e.recordUnder(t, alert, perCustomer, Stored)
	id := e.roomFor(t, perCustomer, "disk-critical", e.org).ID

	blob, codec, err := e.alerts.Evidence(e.ctx, id, alert.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("compressed-evidence"), blob)
	assert.Equal(t, "deflate-1", codec)

	other := e.seed(t, room{ruleID: "cpu-saturated"})
	_, _, err = e.alerts.Evidence(e.ctx, other, alert.ID)
	assert.ErrorIs(t, err, ErrAlertNotFound, "an alert is only readable through the room holding it")

	_, _, err = e.alerts.Evidence(e.ctx, id, uuid.New())
	assert.ErrorIs(t, err, ErrAlertNotFound)
}

// TestEvidenceIsAbsentRatherThanEmpty. A machine that had nothing to attach
// still says it is in trouble, and that alert must read as evidence that does
// not exist rather than as an empty blob under an unnamed codec.
func TestEvidenceIsAbsentRatherThanEmpty(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	alert := e.variant(func(a *Alert) { a.Evidence, a.EvidenceCodec = nil, "" })
	e.recordUnder(t, alert, perCustomer, Stored)
	id := e.roomFor(t, perCustomer, "disk-critical", e.org).ID

	_, _, err := e.alerts.Evidence(e.ctx, id, alert.ID)
	assert.ErrorIs(t, err, ErrNoEvidence)

	room := e.opened(t, id)
	require.Len(t, room.Alerts, 1)
	assert.Empty(t, room.Alerts[0].EvidenceCodec)
	assert.Zero(t, room.Alerts[0].EvidenceBytes)
}

// opened reads one room whole and fails the case if the read itself does, which
