package rules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const eventYAML = `
rules:
  - id: linux-oom-kill
    version: 1
    kind: event
    severity: critical
    summary: The kernel killed a process to reclaim memory.
    group_by: [device]
    group_window_secs: 900
    evidence: [recent_logs]
`

func TestARuleMayWatchTheMachinesOwnWordsInsteadOfAReading(t *testing.T) {
	t.Parallel()

	cat, err := loadFixture(t, eventYAML)
	require.NoError(t, err)

	def, ok := cat.Lookup("linux-oom-kill")
	require.True(t, ok)
	assert.True(t, def.WatchesEvents(),
		"a rule with no reading to compare watches the machine's own words")
	assert.Equal(t, "critical", def.Severity)
	assert.Empty(t, def.Metric, "there is no reading to name")
	assert.Empty(t, def.Tunable, "and no number anybody could retune")
}

func TestARuleAboutWordsCostsTheMachineNothingPerRule(t *testing.T) {
	t.Parallel()

	cat, err := loadFixture(t, eventYAML)
	require.NoError(t, err)
	def, ok := cat.Lookup("linux-oom-kill")
	require.True(t, ok)

	assert.Zero(t, RuleCost(def))
}

func TestLoadCatalogueRefusesAMalformedRuleAboutWords(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		replace [2]string
		because string
	}{
		{
			name:    "no severity",
			replace: [2]string{"    severity: critical\n", ""},
			because: "a queue ordered by severity cannot order a rule that states none",
		},
		{
			name:    "a severity outside the three",
			replace: [2]string{"severity: critical", "severity: catastrophic"},
			because: "a severity nothing downstream can render would be stored happily",
		},
		{
			name:    "no grouping",
			replace: [2]string{"    group_by: [device]\n", ""},
			because: "a rule must say what its alerts are about",
		},
		{
			name:    "a reading it cannot have",
			replace: [2]string{"    kind: event\n", "    kind: event\n    metric: cpu.total\n"},
			because: "a rule watching words watches no reading, and naming one is a rule written wrong",
		},
		{
			name: "a further condition",
			replace: [2]string{"    kind: event\n",
				"    kind: event\n    all:\n      - metric: cpu.total\n        comparator: gt\n        threshold: 1\n        predicate: Instant\n"},
			because: "the reader matches words; there is no reading for a further condition to compare",
		},
		{
			name:    "a number to retune",
			replace: [2]string{"    kind: event\n", "    kind: event\n    tunable:\n      threshold: {min: 1, max: 2}\n"},
			because: "a rule with no line to cross has nothing an operator could move",
		},
		{
			name:    "a kind nobody ships",
			replace: [2]string{"kind: event", "kind: telepathy"},
			because: "a rule this build cannot evaluate is refused rather than ignored",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			broken := strings.Replace(eventYAML, c.replace[0], c.replace[1], 1)
			require.NotEqual(t, eventYAML, broken, "the case must actually break something")
			_, err := loadFixture(t, broken)
			require.Error(t, err, c.because)
			assert.Contains(t, err.Error(), "linux-oom-kill", "the refusal names the rule")
		})
	}
}

func TestEveryRuleAboutAReadingAlsoStatesHowBadItIs(t *testing.T) {
	t.Parallel()

	cat, err := loadFixture(t, validYAML)
	require.NoError(t, err)
	def, ok := cat.Lookup("disk-critical")
	require.True(t, ok)
	assert.Equal(t, "critical", def.Severity)
	assert.Equal(t, protocol.AlertSeverityCritical, def.WireSeverity())
	assert.False(t, def.WatchesEvents())

	silent := strings.Replace(validYAML, "    severity: critical\n", "", 1)
	require.NotEqual(t, validYAML, silent)
	_, err = loadFixture(t, silent)
	require.Error(t, err, "a rule that says nothing about how bad it is cannot be ordered in a queue")
}
