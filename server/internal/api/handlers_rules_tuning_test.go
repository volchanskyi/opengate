package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/rules"
)

func TestATunedValueOutsideTheRulesBoundsIsRefused(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	for name, params := range map[string]map[string]float64{
		"past the rule's ceiling":           {"threshold": 100},
		"something the rule does not offer": {"nonsense": 1},
	} {
		t.Run(name, func(t *testing.T) {
			resp := doRequest(e.srv, http.MethodPut,
				e.query(testPathRules+"/disk-critical/bindings"), e.adminToken,
				RuleBindingInput{
					Level:    RuleBindingLevelOrganization,
					LevelKey: e.org,
					Params:   params,
				})
			assert.Equal(t, http.StatusBadRequest, resp.Code)
		})
	}

	resp := doRequest(e.srv, http.MethodPut,
		e.query(testPathRules+"/no-such-rule/bindings"), e.adminToken,
		RuleBindingInput{Level: RuleBindingLevelOrganization, LevelKey: e.org})
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestTheRulePageExplainsWhereAMachinesNumberCameFrom(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	for _, in := range []RuleBindingInput{
		{Level: RuleBindingLevelOrganization, LevelKey: e.org, Params: map[string]float64{"threshold": 93}},
		{Level: RuleBindingLevelSite, LevelKey: e.site, Params: map[string]float64{"threshold": 95}},
	} {
		resp := doRequest(e.srv, http.MethodPut,
			e.query(testPathRules+"/disk-critical/bindings"), e.adminToken, in)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	}

	page := doRequest(e.srv, http.MethodGet,
		e.query(testPathRules+"/disk-critical"), e.memberToken, nil)
	require.Equal(t, http.StatusOK, page.Code)
	var detail RuleDetail
	require.NoError(t, json.NewDecoder(page.Body).Decode(&detail))
	assert.Len(t, detail.Bindings, 2)
	assert.Equal(t, RuleBindingLevelSite, detail.Bindings[0].Level,
		"the narrowest rung reads first, the way resolution reads it")

	resolved := doRequest(e.srv, http.MethodGet,
		testPathRules+"/disk-critical/resolved?device_id="+e.device.String(), e.memberToken, nil)
	require.Equal(t, http.StatusOK, resolved.Code)
	var got ResolvedRule
	require.NoError(t, json.NewDecoder(resolved.Body).Decode(&got))

	threshold := got.Params["threshold"]
	assert.InEpsilon(t, 95.0, threshold.Value, 0.0001)
	assert.Equal(t, ResolvedRuleParameterLevelSite, threshold.Level)
	assert.Contains(t, threshold.Source, "site")
	assert.True(t, got.Delivered)

	sustain := got.Params["sustain_secs"]
	assert.Equal(t, ResolvedRuleParameterLevelShipped, sustain.Level)
}

func TestResolvingAgainstAnUnknownRuleOrMachine(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	unknownRule := doRequest(e.srv, http.MethodGet,
		testPathRules+"/no-such-rule/resolved?device_id="+e.device.String(), e.memberToken, nil)
	assert.Equal(t, http.StatusNotFound, unknownRule.Code)

	unknownMachine := doRequest(e.srv, http.MethodGet,
		testPathRules+"/disk-critical/resolved?device_id="+uuid.New().String(), e.memberToken, nil)
	assert.Equal(t, http.StatusNotFound, unknownMachine.Code)
}

func TestARulePageSurvivesAnUnreadableTuningStore(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)
	e.srv.ruleAdmin = failingRuleAdmin{RuleAdmin: e.srv.ruleAdmin}

	resp := doRequest(e.srv, http.MethodGet,
		e.query(testPathRules+"/disk-critical"), e.memberToken, nil)
	require.Equal(t, http.StatusOK, resp.Code)

	var detail RuleDetail
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&detail))
	assert.Equal(t, "disk-critical", detail.Rule.Id)
	assert.Empty(t, detail.Bindings)
	assert.Empty(t, detail.Clamps)
}

// failingRuleAdmin answers every read this page makes with an error, and defers
// everything else to the real store.
type failingRuleAdmin struct{ RuleAdmin }

var errStoreDown = errors.New("database is down")

func (failingRuleAdmin) ListBindings(context.Context, uuid.UUID) ([]rules.Binding, error) {
	return nil, errStoreDown
}

func (failingRuleAdmin) ReconcileClamps(context.Context, rules.Pack, uuid.UUID) ([]rules.Clamp, error) {
	return nil, errStoreDown
}

func (failingRuleAdmin) TagsFor(context.Context, uuid.UUID) (map[string]string, error) {
	return nil, errStoreDown
}

func TestAcknowledgingWhatARuleVersionMoved(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)
	ctx := testTenantContext(t)

	tuned := doRequest(e.srv, http.MethodPut,
		e.query(testPathRules+"/disk-critical/bindings"), e.adminToken,
		RuleBindingInput{
			Level:    RuleBindingLevelOrganization,
			LevelKey: e.org,
			Params:   map[string]float64{"threshold": 98},
		})
	require.Equal(t, http.StatusOK, tuned.Code, tuned.Body.String())

	outstanding, err := e.srv.ruleAdmin.ReconcileClamps(ctx, narrowedPack(t), e.org)
	require.NoError(t, err)
	require.Len(t, outstanding, 1)
	clamp := outstanding[0]

	path := testPathRules + "/disk-critical/clamps/" + clamp.ID.String()
	refused := doRequest(e.srv, http.MethodPost, path, e.memberToken, nil)
	assert.Equal(t, http.StatusForbidden, refused.Code)

	acknowledged := doRequest(e.srv, http.MethodPost, path, e.adminToken, nil)
	require.Equal(t, http.StatusNoContent, acknowledged.Code, acknowledged.Body.String())

	require.Eventually(t, func() bool {
		events, err := e.srv.audit.Query(ctx, audit.Query{Action: "rule.clamp.acknowledge", Limit: 10})
		return err == nil && len(events) > 0
	}, auditWaitFor, auditPollEvery, "acknowledging a move must be recorded")

	again := doRequest(e.srv, http.MethodPost, path, e.adminToken, nil)
	assert.Equal(t, http.StatusNotFound, again.Code)
}

// narrowedPack is the shipped disk rule at a later version with a narrower tunable range.
func narrowedPack(t *testing.T) rules.Pack {
	t.Helper()
	cat, err := rules.Embedded()
	require.NoError(t, err)
	def, ok := cat.Lookup("disk-critical")
	require.True(t, ok)

	def.Version++
	def.Tunable = map[string]rules.Bounds{"threshold": {Min: 50, Max: 95}}
	return narrowed{def: def}
}

// narrowed serves exactly one definition, which is all a clamp reconciliation reads.
type narrowed struct{ def rules.Definition }

func (n narrowed) All() []rules.Definition { return []rules.Definition{n.def} }

func (n narrowed) Lookup(id string) (rules.Definition, bool) {
	if id != n.def.ID {
		return rules.Definition{}, false
	}
	return n.def, true
}
