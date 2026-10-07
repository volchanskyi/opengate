package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
)

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
			// A rule reading log records compares no number, so nothing is tunable.
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

// seedRooms writes n further rooms for the customer.
