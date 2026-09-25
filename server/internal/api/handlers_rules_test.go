package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
)

// The rules view: what the curated pack is watching, and what it deliberately
// does not offer.
//
// Coverage is the reason this endpoint exists rather than the catalogue being a
// constant in the client — a rule quietly evaluating on half an estate while
// reading as healthy is the failure the accounting exists to make impossible.

// TestRulesReportCoverageThatAddsUpToTheFleet is the API form of the coverage
// invariant: a rule watching half an estate must say so rather than read as
// healthy, and the only way that is legible is if the states add up to the
// fleet the counts were taken against.
func TestRulesReportCoverageThatAddsUpToTheFleet(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{counts: map[string]agentapi.RuleCoverageCounts{
		"disk-critical": {Active: 1},
		"cpu-saturated": {Unsupported: 1},
	}})

	w := doRequest(e.srv, http.MethodGet,
		"/api/v1/rules?organization_id="+e.org.String(), e.token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var catalogue RuleCatalogue
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &catalogue))
	assert.Equal(t, 1, catalogue.FleetSize)
	require.NotEmpty(t, catalogue.Rules)

	watchingWords := 0
	for _, rule := range catalogue.Rules {
		total := rule.Coverage.Active + rule.Coverage.Throttled +
			rule.Coverage.Unsupported + rule.Coverage.Unknown
		assert.Equalf(t, catalogue.FleetSize, total,
			"rule %s must account for every machine in the estate", rule.Id)
		assert.NotEmpty(t, rule.Summary, "a rule a person reads has to say what it is for")
		assert.NotEmptyf(t, rule.Severity, "rule %s must say how bad it is", rule.Id)

		if rule.Kind == Event {
			// A rule reading the machine's own log records compares no number,
			// so there is nothing to retune and nothing to show a line for.
			// What an administrator can still do is stop it, which is the
			// control that matters for a rule that turns out to be noisy.
			watchingWords++
			assert.Emptyf(t, rule.Tunable, "rule %s watches words, so it has no numbers to retune", rule.Id)
			assert.Nilf(t, rule.Metric, "rule %s watches words, so it names no reading", rule.Id)
			assert.Nilf(t, rule.Threshold, "rule %s watches words, so it has no line to cross", rule.Id)
			continue
		}
		assert.NotEmptyf(t, rule.Tunable, "rule %s watches a reading, so it has numbers a customer may retune", rule.Id)
		assert.NotNilf(t, rule.Metric, "rule %s watches a reading and must name it", rule.Id)
	}
	assert.Positive(t, watchingWords,
		"the screen must show the rules that read the machine's own words, or an administrator cannot stop one")
}

// TestRulesExposeNoAuthoringSurface. Rules are data in a bounded grammar
// compiled into the server, and there is deliberately no way to write one: an
// agent that runs server-supplied code is a supply-chain weapon aimed at every
// customer estate. So the read must not hand back the predicate in a shape that
// implies it can be edited.
func TestRulesExposeNoAuthoringSurface(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})

	w := doRequest(e.srv, http.MethodGet, "/api/v1/rules", e.token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	for _, forbidden := range []string{"\"predicate\"", "\"all\""} {
		assert.NotContains(t, w.Body.String(), forbidden,
			"the catalogue is not an editor: %s belongs to the compiled definition", forbidden)
	}
}

// seedRooms writes n further rooms for the customer, so a case can page a queue
// that holds more than the one room it opened.
