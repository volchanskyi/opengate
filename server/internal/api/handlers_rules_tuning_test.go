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

// The screen that administers detection, driven through the HTTP surface.
//
// Two things matter more than the shapes that come back. Every write is
// administrator-only, refused one endpoint at a time rather than in one combined
// case — a gate that is asserted in aggregate is a gate that can be missing from
// one route and still pass. And every write lands in the audit log, asserted
// table-driven over the whole list, so an endpoint added later without auditing
// fails this test rather than being discovered from an incident nobody can
// attribute.

// How long an audit assertion waits. The write is fire-and-forget by design —
// an audit record must not be able to fail the action it records — so the
// assertion is eventual rather than immediate.

// What a customer may retune, what the screen says about it, and what a rule
// upgrade had to move.
//
// A value the rule would not honour is refused where somebody can still see why,
// and a value a new version no longer allows moves to the nearest one it does —
// while the rule keeps firing, which is the failure the whole clamp exists to
// prevent.

// A value outside what the rule allows is refused where somebody can still see
// why, rather than reaching an estate.
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

// A budget past the maximum the code allows is refused, and so is one of nothing
// — which would silence the customer's detection outright.

// The rule page shows the tuning, and the resolved read answers the question the
// tuning section exists for: why is this machine at this number?
func TestTheRulePageExplainsWhereAMachinesNumberCameFrom(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	// The customer is at 93, the machine's own office at 95. The narrower rung
	// wins, and the screen has to say which one it was.
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
	assert.Contains(t, threshold.Source, "office")
	assert.True(t, got.Delivered)

	// A parameter nobody tuned says so rather than pointing at a rung.
	sustain := got.Params["sustain_secs"]
	assert.Equal(t, ResolvedRuleParameterLevelShipped, sustain.Level)
}

// A stop reaches one customer, and the tenant-wide one reaches every customer at
// once. Both are visible immediately on the read that the delivery path uses.

// Resolving against something that does not exist answers "no such thing"
// rather than an answer about somebody else's estate.
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

// A label and a machine belonging to different customers is refused, and the
// refusal names what was wrong rather than failing the whole request opaquely.

// A rule page whose tuning cannot be read still renders the rule. A read that
// failed costs the page its tuning, not the description somebody opened it for.
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

// A write that names no customer acts on the tenant's own, the same rule a site
// or a device create follows. Without it the whole screen fails whenever the
// picker is showing every customer, which is the state it starts in.

// Acknowledging a move a rule version made: refused to a member, refused when
// nothing is outstanding, and audited when it lands.
func TestAcknowledgingWhatARuleVersionMoved(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)
	ctx := testTenantContext(t)

	// A value the shipped rule allows, and a version of that rule that no longer
	// does — which is what a rule upgrade looks like to everything downstream.
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

	// A second attempt has nothing outstanding to act on, and says so rather
	// than reporting success against a move somebody else already handled.
	again := doRequest(e.srv, http.MethodPost, path, e.adminToken, nil)
	assert.Equal(t, http.StatusNotFound, again.Code)
}

// narrowedPack is the shipped disk rule at a later version that no longer allows
// the value the case tuned.

// narrowedPack is the shipped disk rule at a later version that no longer allows
// the value the case tuned.
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

// narrowed serves exactly one definition, which is the whole of what a clamp
// reconciliation reads.

// narrowed serves exactly one definition, which is the whole of what a clamp
// reconciliation reads.
type narrowed struct{ def rules.Definition }

func (n narrowed) All() []rules.Definition { return []rules.Definition{n.def} }

func (n narrowed) Lookup(id string) (rules.Definition, bool) {
	if id != n.def.ID {
		return rules.Definition{}, false
	}
	return n.def, true
}

// Resolving against something that does not exist answers "no such thing"
// rather than an answer about somebody else's estate.
