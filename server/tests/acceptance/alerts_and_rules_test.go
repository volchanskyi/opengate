package acceptance

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// rulesFor picks one rule out of what the product pushed to a machine.
func rulesFor(msg *protocol.ControlMessage, ruleID string) (protocol.ThresholdRule, bool) {
	for _, rule := range msg.AlertRules {
		if rule.ID == ruleID {
			return rule, true
		}
	}
	return protocol.ThresholdRule{}, false
}

// TestARuleReachesAMachineAndItsBreachComesBackAsAnAlert is the sentence
// Alerts and Rules promises. The detection half is proven on the agent side
// and the filing half on the server side; what neither can show is that the
// rule a machine is running is the rule this product sent it, and that the
// alert it raises against that rule is the one the customer's queue receives.
func TestARuleReachesAMachineAndItsBreachComesBackAsAnAlert(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-build-agent",
		protocol.CapTerminal, protocol.CapThresholdAlerts)
	machine.AwaitOnline()

	pushed := machine.Await(protocol.MsgPushAlertRules)
	rule, carried := rulesFor(pushed, triageRule)
	require.Truef(t, carried, "the machine must be given %s to watch for", triageRule)
	assert.Equal(t, "cpu.total", rule.Metric)
	assert.Positive(t, rule.Threshold, "a rule with no threshold watches for nothing")
	assert.Positive(t, pushed.DeviceHourlyCeiling,
		"the machine is told its allowance, because the flood is stopped where it starts")

	machine.raiseAlert("indexer")

	room := admin.awaitIncident()
	assert.Equal(t, rule.ID, room.RuleID,
		"the rule the machine was given is the rule the customer's queue names")
}

// TestATunedThresholdReachesOneCustomerAndNotTheOther is the sentence Rule
// Administration promises. Retuning is per-customer, and the proof that it is
// has to be a machine in another customer still running the shipped default —
// a check against the stored binding would pass even if the value never
// travelled.
func TestATunedThresholdReachesOneCustomerAndNotTheOther(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	fabrikam := product.arrangeCustomer("Fabrikam")
	admin := product.Administrator(contoso)
	token := admin.mintEnrolmentToken("rollout").Token

	// A machine that stays in Contoso, and one moved to Fabrikam.
	fabrikamMachine := product.Machine(token, "fabrikam-build-agent",
		protocol.CapTerminal, protocol.CapThresholdAlerts)
	fabrikamMachine.AwaitOnline()
	require.Equal(t, http.StatusOK, admin.fileUnder(fabrikamMachine, fabrikam).Status)

	const tuned = 62
	require.Equal(t, http.StatusOK, admin.tuneRule(triageRule, contoso, tuned).Status)

	contosoMachine := product.Machine(token, "contoso-build-agent",
		protocol.CapTerminal, protocol.CapThresholdAlerts)
	contosoMachine.AwaitOnline()

	inContoso, carried := rulesFor(contosoMachine.Await(protocol.MsgPushAlertRules), triageRule)
	require.True(t, carried)
	assert.InDelta(t, float64(tuned), inContoso.Threshold, 0.001,
		"Contoso's machines run Contoso's number")

	// The machine that moved to Fabrikam re-reads its rules when it reconnects.
	fabrikamMachine.Disconnect()
	rejoined := product.MachineWithIdentity(token, fabrikamMachine.DeviceID, "fabrikam-build-agent",
		protocol.CapTerminal, protocol.CapThresholdAlerts)
	rejoined.AwaitOnline()

	inFabrikam, carried := rulesFor(rejoined.Await(protocol.MsgPushAlertRules), triageRule)
	require.True(t, carried)
	assert.NotEqual(t, float64(tuned), inFabrikam.Threshold,
		"another customer's machines are untouched by Contoso's retuning")
}

// TestAStopSwitchReachesMachinesAlreadyCarryingTheRule is the mitigation for a
// rule that turns out to be wrong. It has to take effect on the estate that is
// already running it, without a deploy and without waiting for anything.
func TestAStopSwitchReachesMachinesAlreadyCarryingTheRule(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-build-agent",
		protocol.CapTerminal, protocol.CapThresholdAlerts)
	machine.AwaitOnline()

	_, carried := rulesFor(machine.Await(protocol.MsgPushAlertRules), triageRule)
	require.True(t, carried, "the machine starts out watching for it")

	reply := admin.Post(admin.InCustomer("/api/v1/rules/"+triageRule+"/stop"),
		map[string]any{"scope": "organization", "stopped": true})
	require.Equalf(t, http.StatusNoContent, reply.Status, "stopping the rule failed: %s", reply.Text())

	require.Eventuallyf(t, func() bool {
		machine.Disconnect()
		rejoined := product.MachineWithIdentity(
			admin.mintEnrolmentToken("Head Office").Token, machine.DeviceID, "contoso-build-agent",
			protocol.CapTerminal, protocol.CapThresholdAlerts)
		rejoined.AwaitOnline()
		_, still := rulesFor(rejoined.Await(protocol.MsgPushAlertRules), triageRule)
		return !still
	}, eventually, poll, "a stopped rule must stop being sent to the estate")
}

// tuneRule files a customer's own value for one of the rule's parameters,
// which is the whole of what retuning is.
func (a *Technician) tuneRule(ruleID string, customer uuid.UUID, threshold float64) Reply {
	a.t.Helper()
	return a.Put("/api/v1/rules/"+ruleID+"/bindings", map[string]any{
		"level":     "organization",
		"level_key": customer.String(),
		"params":    map[string]float64{"threshold": threshold},
	})
}
