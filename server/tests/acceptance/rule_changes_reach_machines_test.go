package acceptance

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// A rule runs on a customer's own machines, on processor time they pay for, so
// a rule that turns out to be wrong has to be stoppable without waiting for
// anything. A link is held open for as long as it is healthy, which means a
// change delivered only on registration is delivered only when something
// unrelated breaks it — long enough for somebody to switch a rule off, watch
// the screen say Stopped, and have it go on firing every night.

// TestAStoppedRuleReachesAMachineThatNeverDisconnects is the sentence Rule
// Administration promises: stopping a rule needs no deploy, and no waiting.
//
// Priya switches off a rule that fires on Contoso's Derby plant machines every
// night at 03:00 during the backup window. All 240 of her machines are
// connected and stay connected — a healthy link is held open indefinitely — so
// a change that waited for the next registration would wait for something
// unrelated to break it, while her screen said Stopped and the queue filled
// again every night.
func TestAStoppedRuleReachesAMachineThatNeverDisconnects(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-fs01",
		protocol.CapTerminal, protocol.CapThresholdAlerts, protocol.CapAlerts)
	machine.AwaitOnline()

	_, carried := rulesFor(machine.Await(protocol.MsgPushAlertRules), triageRule)
	require.True(t, carried, "the machine starts out watching for it")

	// Marked before the switch is thrown: the product delivers the change while
	// the request is still open, so a count taken afterwards has already
	// swallowed the push this case is waiting for.
	delivered := machine.PushesSoFar(protocol.MsgPushAlertRules)

	reply := admin.Post(admin.InCustomer("/api/v1/rules/"+triageRule+"/stop"),
		map[string]any{"scope": "organization", "stopped": true})
	require.Equalf(t, http.StatusNoContent, reply.Status, "stopping the rule failed: %s", reply.Text())

	// The machine is given the ruleset as it now stands, on the connection it
	// has been holding the whole time. Nothing was disconnected to make this
	// happen.
	_, still := rulesFor(machine.AwaitPast(protocol.MsgPushAlertRules, delivered), triageRule)
	assert.False(t, still, "the rule the administrator stopped is gone from what the machine runs")
}

// And a retuned number reaches it the same way. A threshold nobody's machines
// are comparing against is a row in a table: Contoso's file servers legitimately
// sit at 92% disk, and raising their line to 95 is no use to them next week.
func TestARetunedThresholdReachesAMachineThatNeverDisconnects(t *testing.T) {
	t.Parallel()

	const retuned = 77

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-fs01",
		protocol.CapTerminal, protocol.CapThresholdAlerts, protocol.CapAlerts)
	machine.AwaitOnline()

	shipped, carried := rulesFor(machine.Await(protocol.MsgPushAlertRules), triageRule)
	require.True(t, carried)
	require.NotEqualf(t, float64(retuned), shipped.Threshold,
		"the case is only about the change if the shipped number is a different one")

	delivered := machine.PushesSoFar(protocol.MsgPushAlertRules)

	tuned := admin.Put(admin.InCustomer("/api/v1/rules/"+triageRule+"/bindings"), map[string]any{
		"level":     "organization",
		"level_key": contoso.String(),
		"params":    map[string]float64{"threshold": float64(retuned)},
	})
	require.Equalf(t, http.StatusOK, tuned.Status, "retuning the rule failed: %s", tuned.Text())

	updated, carried := rulesFor(machine.AwaitPast(protocol.MsgPushAlertRules, delivered), triageRule)
	require.True(t, carried)
	assert.InDelta(t, float64(retuned), updated.Threshold, 0.001,
		"the machine compares against the number the administrator set, from now rather than from next week")
}
