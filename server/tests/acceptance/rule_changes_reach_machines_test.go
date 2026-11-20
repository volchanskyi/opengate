package acceptance

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// takesRules is what a machine that watches the readings it is given and raises alerts registers.
var takesRules = []protocol.AgentCapability{protocol.CapTerminal, protocol.CapThresholdAlerts, protocol.CapAlerts}

func TestAStoppedRuleReachesAMachineThatNeverDisconnects(t *testing.T) {
	t.Parallel()

	admin, machine := contosoMachine(t, newProduct(t), "Head Office", "contoso-fs01", takesRules...)

	_, carried := rulesFor(machine.Await(protocol.MsgPushAlertRules), triageRule)
	require.True(t, carried, "the machine starts out watching for it")

	// The mark precedes the request because the push arrives while the request is still open.
	delivered := machine.PushesSoFar(protocol.MsgPushAlertRules)

	reply := admin.Post(admin.InCustomer("/api/v1/rules/"+triageRule+"/stop"),
		map[string]any{"scope": "organization", "stopped": true})
	require.Equalf(t, http.StatusNoContent, reply.Status, "stopping the rule failed: %s", reply.Text())

	_, still := rulesFor(machine.AwaitPast(protocol.MsgPushAlertRules, delivered), triageRule)
	assert.False(t, still, "the rule the administrator stopped is gone from what the machine runs")
}

func TestARetunedThresholdReachesAMachineThatNeverDisconnects(t *testing.T) {
	t.Parallel()

	const retuned = 77

	admin, machine := contosoMachine(t, newProduct(t), "Head Office", "contoso-fs01", takesRules...)

	shipped, carried := rulesFor(machine.Await(protocol.MsgPushAlertRules), triageRule)
	require.True(t, carried)
	require.NotEqualf(t, float64(retuned), shipped.Threshold,
		"the case is only about the change if the shipped number is a different one")

	delivered := machine.PushesSoFar(protocol.MsgPushAlertRules)

	tuned := admin.Put(admin.InCustomer("/api/v1/rules/"+triageRule+"/bindings"), map[string]any{
		"level":     "organization",
		"level_key": admin.Customer.String(),
		"params":    map[string]float64{"threshold": float64(retuned)},
	})
	require.Equalf(t, http.StatusOK, tuned.Status, "retuning the rule failed: %s", tuned.Text())

	updated, carried := rulesFor(machine.AwaitPast(protocol.MsgPushAlertRules, delivered), triageRule)
	require.True(t, carried)
	assert.InDelta(t, float64(retuned), updated.Threshold, 0.001,
		"the machine compares against the number the administrator set, from now rather than from next week")
}

func TestAChangedAlertBudgetReachesAMachineThatNeverDisconnects(t *testing.T) {
	t.Parallel()

	const budget = 7

	admin, machine := contosoMachine(t, newProduct(t), "Head Office", "contoso-fs01", takesRules...)

	first := machine.Await(protocol.MsgPushAlertRules)
	require.NotEqualf(t, uint32(budget), first.DeviceHourlyCeiling,
		"the case is only about the change if the machine starts on a different allowance")

	delivered := machine.PushesSoFar(protocol.MsgPushAlertRules)

	set := admin.Put(admin.InCustomer("/api/v1/alert-limits"), map[string]any{
		"organization_hourly": 500,
		"device_hourly":       budget,
	})
	require.Equalf(t, http.StatusOK, set.Status, "setting the alert budget failed: %s", set.Text())

	updated := machine.AwaitPast(protocol.MsgPushAlertRules, delivered)
	assert.EqualValues(t, budget, updated.DeviceHourlyCeiling,
		"the machine holds itself to the allowance the administrator set, from now rather than its next start")
}

func TestARemovedTuningReachesAMachineThatNeverDisconnects(t *testing.T) {
	t.Parallel()

	const retuned = 77

	admin, machine := contosoMachine(t, newProduct(t), "Head Office", "contoso-fs01", takesRules...)

	shipped, carried := rulesFor(machine.Await(protocol.MsgPushAlertRules), triageRule)
	require.True(t, carried)
	require.NotEqualf(t, float64(retuned), shipped.Threshold,
		"the case is only about the removal if the tuned number differs from the shipped one")

	delivered := machine.PushesSoFar(protocol.MsgPushAlertRules)
	var binding struct {
		ID string `json:"id"`
	}
	tuned := admin.Put(admin.InCustomer("/api/v1/rules/"+triageRule+"/bindings"), map[string]any{
		"level":     "organization",
		"level_key": admin.Customer.String(),
		"params":    map[string]float64{"threshold": float64(retuned)},
	}).Into(&binding)
	require.Equalf(t, http.StatusOK, tuned.Status, "retuning the rule failed: %s", tuned.Text())
	updated, _ := rulesFor(machine.AwaitPast(protocol.MsgPushAlertRules, delivered), triageRule)
	require.InDelta(t, float64(retuned), updated.Threshold, 0.001)

	delivered = machine.PushesSoFar(protocol.MsgPushAlertRules)
	removed := admin.Delete("/api/v1/rules/" + triageRule + "/bindings/" + binding.ID)
	require.Equalf(t, http.StatusNoContent, removed.Status, "removing the tuning failed: %s", removed.Text())

	restored, carried := rulesFor(machine.AwaitPast(protocol.MsgPushAlertRules, delivered), triageRule)
	require.True(t, carried)
	assert.InDelta(t, shipped.Threshold, restored.Threshold, 0.001,
		"the machine goes back to the shipped number once the administrator removes the tuning")
}
