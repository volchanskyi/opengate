package acceptance

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

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
