package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestABudgetOutsideItsBoundsIsRefused(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	for name, in := range map[string]AlertLimitsInput{
		"past the customer maximum": {
			OrganizationHourly: alerts.MaxOrganizationHourlyCeiling + 1,
			DeviceHourly:       alerts.DefaultDeviceHourlyCeiling,
		},
		"past the machine maximum": {
			OrganizationHourly: alerts.DefaultOrganizationHourlyCeiling,
			DeviceHourly:       alerts.MaxDeviceHourlyCeiling + 1,
		},
		"nothing at all": {OrganizationHourly: 0, DeviceHourly: 0},
	} {
		t.Run(name, func(t *testing.T) {
			resp := doRequest(e.srv, http.MethodPut, e.query(testPathLimits), e.adminToken, in)
			assert.Equal(t, http.StatusBadRequest, resp.Code)
		})
	}

	resp := doRequest(e.srv, http.MethodPut, e.query(testPathLimits), e.adminToken,
		AlertLimitsInput{
			OrganizationHourly: alerts.MaxOrganizationHourlyCeiling,
			DeviceHourly:       alerts.MaxDeviceHourlyCeiling,
		})
	require.Equal(t, http.StatusOK, resp.Code)

	var stored AlertLimits
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&stored))
	assert.Equal(t, alerts.MaxOrganizationHourlyCeiling, stored.OrganizationHourly)
	assert.Equal(t, alerts.MaxOrganizationHourlyCeiling, stored.MaxOrganizationHourly)
	assert.Equal(t, alerts.MaxDeviceHourlyCeiling, stored.MaxDeviceHourly)
}

func TestAnUnconfiguredBudgetReadsAsTheShippedOne(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	resp := doRequest(e.srv, http.MethodGet, e.query(testPathLimits), e.memberToken, nil)
	require.Equal(t, http.StatusOK, resp.Code)

	var got AlertLimits
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, alerts.DefaultOrganizationHourlyCeiling, got.OrganizationHourly)
	assert.Equal(t, alerts.DefaultDeviceHourlyCeiling, got.DeviceHourly)
}

func TestARolloutPaceOutsideItsBoundsIsRefused(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	for name, in := range map[string]RuleRolloutInput{
		"a canary reaching everybody": {
			Enabled: true, CanaryPercent: 100, StagedPercent: 10,
			CanaryHoldSecs: 3600, StagedHoldSecs: 21600,
		},
		"a canary bigger than the stage after it": {
			Enabled: true, CanaryPercent: 40, StagedPercent: 20,
			CanaryHoldSecs: 3600, StagedHoldSecs: 21600,
		},
		"a hold of seconds": {
			Enabled: true, CanaryPercent: 1, StagedPercent: 10,
			CanaryHoldSecs: 1, StagedHoldSecs: 21600,
		},
	} {
		t.Run(name, func(t *testing.T) {
			resp := doRequest(e.srv, http.MethodPut,
				e.query(testPathRules+"/disk-critical/rollout"), e.adminToken, in)
			assert.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
		})
	}
}

func TestAStopReachesOneCustomerOrEveryCustomer(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)
	ctx := testTenantContext(t)
	other := testutil.SeedOrganization(t, ctx, e.srv.store, "fabrikam")

	stop := func(scope RuleStopScope, ruleID string) {
		resp := doRequest(e.srv, http.MethodPost,
			e.query(testPathRules+"/"+ruleID+"/stop"), e.adminToken,
			RuleStopInput{Scope: scope, Stopped: true})
		require.Equal(t, http.StatusNoContent, resp.Code, resp.Body.String())
	}

	stop(RuleStopScopeOrganization, "disk-critical")
	assert.True(t, e.killed(t, e.org, "disk-critical"))
	assert.False(t, e.killed(t, other, "disk-critical"),
		"stopping one customer's rule must not touch another's")

	stop(RuleStopScopeTenant, "cpu-saturated")
	assert.True(t, e.killed(t, e.org, "cpu-saturated"))
	assert.True(t, e.killed(t, other, "cpu-saturated"),
		"the tenant-wide stop reaches every customer")

	resp := doRequest(e.srv, http.MethodPost,
		e.query(testPathRules+"/disk-critical/stop"), e.adminToken,
		RuleStopInput{Scope: RuleStopScopeOrganization, Stopped: false})
	require.Equal(t, http.StatusNoContent, resp.Code)
	assert.False(t, e.killed(t, e.org, "disk-critical"))
}

// killed reads whether one customer's rule is stopped, from the store the delivery path reads.
func (e ruleAdminEstate) killed(t *testing.T, org uuid.UUID, ruleID string) bool {
	t.Helper()
	stored, err := e.srv.ruleRollouts.ListRollouts(testTenantContext(t), org)
	require.NoError(t, err)
	return stored[ruleID].Kill
}

func TestLabelsAreCustomerScopedAndHeldByTheRulesThatAimAtThem(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	created := doRequest(e.srv, http.MethodPost,
		e.query(testPathDeviceTags+"/labels"), e.adminToken,
		DeviceTagLabelInput{Key: "role", Value: "file-server"})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var label DeviceTagLabel
	require.NoError(t, json.NewDecoder(created.Body).Decode(&label))

	assigned := doRequest(e.srv, http.MethodPut, testPathDeviceTags+"/assignments", e.adminToken,
		DeviceTagAssignmentInput{DeviceIds: []uuid.UUID{e.device}, LabelId: label.Id})
	require.Equal(t, http.StatusNoContent, assigned.Code, assigned.Body.String())

	listed := doRequest(e.srv, http.MethodGet, e.query(testPathDeviceTags), e.memberToken, nil)
	require.Equal(t, http.StatusOK, listed.Code)
	var catalogue DeviceTagCatalogue
	require.NoError(t, json.NewDecoder(listed.Body).Decode(&catalogue))
	require.Len(t, catalogue.Labels, 1)
	require.Len(t, catalogue.Assignments, 1)
	assert.Equal(t, map[string]string{"role": "file-server"}, catalogue.Assignments[0].Tags)

	aim := doRequest(e.srv, http.MethodPut,
		e.query(testPathRules+"/disk-critical/bindings"), e.adminToken,
		RuleBindingInput{
			Level:    RuleBindingLevelOrganization,
			LevelKey: e.org,
			Selector: &map[string]string{"role": "file-server"},
			Params:   map[string]float64{"threshold": 95},
		})
	require.Equal(t, http.StatusOK, aim.Code, aim.Body.String())

	refused := doRequest(e.srv, http.MethodDelete,
		testPathDeviceTags+"/labels/"+label.Id.String(), e.adminToken, nil)
	assert.Equal(t, http.StatusConflict, refused.Code)

	missing := doRequest(e.srv, http.MethodDelete,
		testPathDeviceTags+"/labels/"+uuid.New().String(), e.adminToken, nil)
	assert.Equal(t, http.StatusNotFound, missing.Code)
}

func TestAssigningALabelAcrossCustomersIsRefused(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)
	ctx := testTenantContext(t)

	other := testutil.SeedOrganization(t, ctx, e.srv.store, "fabrikam")
	otherSite := testutil.SeedSiteIn(t, ctx, e.srv.store, other)
	otherDevice := testutil.SeedDeviceIn(t, ctx, e.srv.store, other, otherSite.ID)

	created := doRequest(e.srv, http.MethodPost,
		e.query(testPathDeviceTags+"/labels"), e.adminToken,
		DeviceTagLabelInput{Key: "env", Value: "production"})
	require.Equal(t, http.StatusCreated, created.Code)
	var label DeviceTagLabel
	require.NoError(t, json.NewDecoder(created.Body).Decode(&label))

	refused := doRequest(e.srv, http.MethodPut, testPathDeviceTags+"/assignments", e.adminToken,
		DeviceTagAssignmentInput{DeviceIds: []uuid.UUID{otherDevice.ID}, LabelId: label.Id})
	assert.Equal(t, http.StatusBadRequest, refused.Code)
}

func TestAnUnknownRuleIsRefusedEverywhereItCanBeNamed(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	for name, probe := range map[string]struct {
		method string
		path   string
		body   any
	}{
		"reading it":  {http.MethodGet, e.query(testPathRules + "/no-such-rule"), nil},
		"tuning it":   {http.MethodPut, e.query(testPathRules + "/no-such-rule/bindings"), RuleBindingInput{Level: RuleBindingLevelOrganization, LevelKey: e.org}},
		"rolling it":  {http.MethodPut, e.query(testPathRules + "/no-such-rule/rollout"), RuleRolloutInput{Enabled: true, CanaryPercent: 1, StagedPercent: 10, CanaryHoldSecs: 3600, StagedHoldSecs: 21600}},
		"stopping it": {http.MethodPost, e.query(testPathRules + "/no-such-rule/stop"), RuleStopInput{Scope: RuleStopScopeOrganization, Stopped: true}},
	} {
		t.Run(name, func(t *testing.T) {
			resp := doRequest(e.srv, probe.method, probe.path, e.adminToken, probe.body)
			assert.Equal(t, http.StatusNotFound, resp.Code)
		})
	}
}

func TestAWriteNamingNoCustomerActsOnTheTenantsOwn(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	tuned := doRequest(e.srv, http.MethodPut,
		testPathRules+"/disk-critical/bindings", e.adminToken,
		RuleBindingInput{
			Level:    RuleBindingLevelOrganization,
			LevelKey: e.org,
			Params:   map[string]float64{"threshold": 95},
		})
	require.Equal(t, http.StatusOK, tuned.Code, tuned.Body.String())

	budget := doRequest(e.srv, http.MethodPut, testPathLimits, e.adminToken,
		AlertLimitsInput{OrganizationHourly: 900, DeviceHourly: 30})
	require.Equal(t, http.StatusOK, budget.Code, budget.Body.String())

	label := doRequest(e.srv, http.MethodPost, testPathDeviceTags+"/labels", e.adminToken,
		DeviceTagLabelInput{Key: "env", Value: "production"})
	require.Equal(t, http.StatusCreated, label.Code, label.Body.String())

	rollout := doRequest(e.srv, http.MethodPut,
		testPathRules+"/disk-critical/rollout", e.adminToken,
		RuleRolloutInput{
			Enabled: true, CanaryPercent: 1, StagedPercent: 10,
			CanaryHoldSecs: 3600, StagedHoldSecs: 21600,
		})
	require.Equal(t, http.StatusOK, rollout.Code, rollout.Body.String())

	stop := doRequest(e.srv, http.MethodPost,
		testPathRules+"/disk-critical/stop", e.adminToken,
		RuleStopInput{Scope: RuleStopScopeOrganization, Stopped: true})
	require.Equal(t, http.StatusNoContent, stop.Code, stop.Body.String())

	assert.True(t, e.killed(t, e.org, "disk-critical"))

	listed := doRequest(e.srv, http.MethodGet, testPathRules, e.adminToken, nil)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var catalogue RuleCatalogue
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &catalogue))
	var diskCritical *Rule
	for i := range catalogue.Rules {
		if catalogue.Rules[i].Id == "disk-critical" {
			diskCritical = &catalogue.Rules[i]
		}
	}
	require.NotNil(t, diskCritical)
	assert.True(t, diskCritical.Rollout.Kill, "the list shows the stop")
	assert.True(t, diskCritical.Rollout.Enabled, "and the rollout it was given")

	detail := doRequest(e.srv, http.MethodGet, testPathRules+"/disk-critical", e.adminToken, nil)
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	var shown RuleDetail
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &shown))
	assert.True(t, shown.Rule.Rollout.Kill, "the rule's own page shows the stop")
	assert.NotEmpty(t, shown.Bindings, "and the tuning made for that customer")
}
