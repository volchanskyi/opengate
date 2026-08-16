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

	for _, rule := range catalogue.Rules {
		total := rule.Coverage.Active + rule.Coverage.Throttled +
			rule.Coverage.Unsupported + rule.Coverage.Unknown
		assert.Equalf(t, catalogue.FleetSize, total,
			"rule %s must account for every machine in the estate", rule.Id)
		assert.NotEmpty(t, rule.Summary, "a rule a person reads has to say what it is for")
		assert.NotEmpty(t, rule.Tunable, "the numbers a customer may retune are what this surface is for")
	}
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
