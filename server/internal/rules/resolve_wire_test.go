package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// What a rule carries onto the wire beyond its numbers.
//
// A machine can only state what it was told. Both fields here are things the
// far end refuses an alert for not stating, so a rule that reached a machine
// without them would be a rule that machine can raise nothing from.

// An alert's identity is the machine, the rule, the rule's revision and the
// window it fired for, and the machine cannot state a revision nobody told it.
// A retuned number is not a new revision — the revision says which definition
// fired, so an alert raised last week still means what it meant then.
func TestTheRuleReachingAMachineCarriesItsRevision(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	c := newContoso()

	shipped := Resolve(def, c.laptop, nil)
	assert.Equal(t, uint32(def.Version), shipped.Version,
		"a machine cannot state a revision it was never sent")

	retuned := Resolve(def, c.fs01, []Binding{orgBinding(c.org, def.ID, threshold(95))})
	assert.Equal(t, uint32(def.Version), retuned.Version,
		"retuning a number is not a new definition")
	assert.InDelta(t, 95.0, retuned.Threshold, 0.001, "and the retuned number still travels")
}

// A rule's severity is what orders a technician's queue, so it travels to the
// machine with the rule and the machine states it on every alert it raises.
// An alert that arrived saying nothing about how bad it is would file a disk
// about to stop accepting writes beside one that merely feels slow.
func TestTheRuleReachingAMachineCarriesHowBadItIs(t *testing.T) {
	t.Parallel()

	c := newContoso()
	full := Resolve(diskCritical(t), c.fs01, nil)
	slow := Resolve(shippedRule(t, "disk-slow"), c.fs01, nil)

	assert.Equal(t, protocol.AlertSeverityCritical, full.Severity,
		"a disk about to stop accepting writes is not the same news as a slow one")
	assert.Equal(t, protocol.AlertSeverityWarning, slow.Severity)
}
