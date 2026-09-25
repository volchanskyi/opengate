package rules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// --- rules about the machine's own words ---
//
// Some failures never cross a line, because nothing about them is a number: a
// process killed to reclaim memory, a disk that stopped answering its bus. The
// machine reports each of those about itself, in words, in its own log, and a
// curated pack of matchers reads them there. The matching stays on the machine
// — the phrases are what the reader is built around — so what the file below
// carries is the rest of a rule: what it is called, which revision it is, how
// bad it is, and where its alerts belong.
//
// Without these rows the server has never heard of the rule, refuses every
// alert it raises, and the machine's findings are discarded — which is exactly
// what was happening.

// eventYAML is the shape of a rule that watches words rather than a reading.
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

// The machine evaluates the whole log pack on one bounded poll a minute, so a
// further rule in it costs nothing further. Counting each one against the
// endpoint budget would refuse a pack the machine reads for free.
func TestARuleAboutWordsCostsTheMachineNothingPerRule(t *testing.T) {
	t.Parallel()

	cat, err := loadFixture(t, eventYAML)
	require.NoError(t, err)
	def, ok := cat.Lookup("linux-oom-kill")
	require.True(t, ok)

	assert.Zero(t, RuleCost(def))
}

// Each of these is a rule the fleet could not act on, so each is refused at
// load rather than discovered by whoever opens the incident.
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
		})
	}
}

// A rule about a reading still has to state how bad it is. Severity is what
// orders the queue, and a rule that says nothing would file a full disk beside
// a sluggish one.
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
