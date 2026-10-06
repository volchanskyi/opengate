package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

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

func TestTheRuleReachingAMachineCarriesHowBadItIs(t *testing.T) {
	t.Parallel()

	c := newContoso()
	full := Resolve(diskCritical(t), c.fs01, nil)
	slow := Resolve(shippedRule(t, "disk-slow"), c.fs01, nil)

	assert.Equal(t, protocol.AlertSeverityCritical, full.Severity,
		"a disk about to stop accepting writes is not the same news as a slow one")
	assert.Equal(t, protocol.AlertSeverityWarning, slow.Severity)
}

func TestARevisionTheWireCannotCarryIsCarriedAsNothing(t *testing.T) {
	t.Parallel()

	for version, want := range map[int]uint32{
		-1:                      0,
		0:                       0,
		1:                       1,
		int(maxRuleVersion):     uint32(maxRuleVersion),
		int(maxRuleVersion) + 1: 0,
	} {
		assert.Equal(t, want, wireVersion(version), "revision %d", version)
	}
}
