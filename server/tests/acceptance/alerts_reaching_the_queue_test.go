package acceptance

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// The sentences that only became true once an alert a machine raises reached
// the queue: a failure that crosses no line, a finding out of history, a rule a
// customer stopped, and a change reaching a machine that never disconnects.

// wordRule is a rule the machine's own log reader carries. The phrases it
// matches live on the machine; what the product holds is the rest of it, which
// is what lets an alert naming it be accepted, placed in a room, and stopped.
const wordRule = "linux-oom-kill"

// TestAFailureThatCrossesNoLineStillReachesTheQueue is the second sentence
// Alerts and Rules promises.
//
// CONTOSO-SQL02 has its reporting service killed at 02:14 to reclaim memory.
// Memory drops back to normal the instant the process dies, so no rule about a
// reading can ever see it — the machine says so in its own log and nowhere
// else. That class of failure is the whole reason the log rules exist, and an
// alert from one has to be something a technician can work.
func TestAFailureThatCrossesNoLineStillReachesTheQueue(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-sql02",
		protocol.CapTerminal, protocol.CapThresholdAlerts, protocol.CapAlerts)
	machine.AwaitOnline()
	machine.Await(protocol.MsgPushAlertRules)

	machine.raiseWordAlert(wordRule)

	room := admin.awaitIncident()
	assert.Equal(t, wordRule, room.RuleID,
		"a failure the machine reported in words is a room like any other")
	assert.Equal(t, "critical", room.Severity,
		"and it is as bad as the rule says, so the queue can be ordered by it")
}

// TestARuleTheCustomerStoppedRaisesNothingInTheirQueue is the switch on the
// administration screen actually doing something.
//
// A rule about a reading is stopped by never being sent. A rule the machine's
// log reader carries cannot be stopped that way — the machine goes on matching
// — so the stop is applied where the alert arrives. Without that, the switch
// would be a switch that changes nothing.
func TestARuleTheCustomerStoppedRaisesNothingInTheirQueue(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	reply := admin.Post(admin.InCustomer("/api/v1/rules/"+wordRule+"/stop"),
		map[string]any{"scope": "organization", "stopped": true})
	require.Equalf(t, http.StatusNoContent, reply.Status, "stopping a rule failed: %s", reply.Text())

	// The decision reaches a machine as it connects, the same moment a stopped
	// rule about a reading stops being sent to one.
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-sql02",
		protocol.CapTerminal, protocol.CapThresholdAlerts, protocol.CapAlerts)
	machine.AwaitOnline()
	machine.Await(protocol.MsgPushAlertRules)

	machine.raiseWordAlert(wordRule)
	// And one from a rule they did not stop, raised after it. Waiting for this
	// one to arrive is what makes the other one's absence a fact rather than a
	// clock reading: the machine and the product have both finished with the
	// first by the time the second has landed.
	machine.raiseAlert("indexer")

	room := admin.awaitIncident()
	assert.Equal(t, triageRule, room.RuleID)
	assert.Len(t, admin.triageQueue(), 1,
		"a customer who stopped a rule receives nothing from it")
}

// TestAFindingOutOfHistoryBelongsWhereItHappened is what makes the "has this
// happened before?" answer usable.
//
// A new rule reaches Contoso's twelve refurbished laptops and each one re-runs
// it over the months of readings it already holds. A freeze from three weeks
// ago has to stay three weeks old: stamped today it would sort to the top of
// the queue beside a machine that is failing right now, and a whole scan would
// read as a fleet-wide outage happening this minute.
func TestAFindingOutOfHistoryBelongsWhereItHappened(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-laptop-07",
		protocol.CapTerminal, protocol.CapThresholdAlerts, protocol.CapAlerts)
	machine.AwaitOnline()
	machine.Await(protocol.MsgPushAlertRules)

	threeWeeksAgo := time.Now().UTC().Truncate(time.Second).Add(-21 * 24 * time.Hour)
	machine.raiseFinding(triageRule, threeWeeksAgo)

	room := admin.awaitIncident()
	assert.Equal(t, triageRule, room.RuleID)

	opened := admin.openIncident(room.ID)
	require.NotEmpty(t, opened.Alerts, "the room holds the finding that opened it")
	assert.True(t, opened.Alerts[0].Backfilled,
		"a finding out of history says which it is, because it reads completely differently")
	assert.WithinDuration(t, threeWeeksAgo, opened.Alerts[0].ObservedAt, time.Minute,
		"stamped with the minute it happened, not the minute the scan found it")
}
