package acceptance

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// wordRule names a rule carried by the machine's own log reader, matched on the machine.
const wordRule = "linux-oom-kill"

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

func TestARuleTheCustomerStoppedRaisesNothingInTheirQueue(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	reply := admin.Post(admin.InCustomer("/api/v1/rules/"+wordRule+"/stop"),
		map[string]any{"scope": "organization", "stopped": true})
	require.Equalf(t, http.StatusNoContent, reply.Status, "stopping a rule failed: %s", reply.Text())

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-sql02",
		protocol.CapTerminal, protocol.CapThresholdAlerts, protocol.CapAlerts)
	machine.AwaitOnline()
	machine.Await(protocol.MsgPushAlertRules)

	machine.raiseWordAlert(wordRule)
	// Waiting for this later alert proves the stopped rule's alert was already processed.
	machine.raiseAlert("indexer")

	room := admin.awaitIncident()
	assert.Equal(t, triageRule, room.RuleID)
	assert.Len(t, admin.triageQueue(), 1,
		"a customer who stopped a rule receives nothing from it")
}

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
