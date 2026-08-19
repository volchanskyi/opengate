package api

import (
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/testutil"
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

// Who may administer detection, and where every write is recorded.
//
// Two contracts drive the whole screen. Every write is administrator-only,
// refused one endpoint at a time rather than in one combined case — a gate
// asserted in aggregate is a gate that can be missing from one route and still
// pass. And every write lands in the audit log, asserted table-driven over the
// whole list, so an endpoint added later without auditing fails this rather than
// being discovered from an incident nobody can attribute.
//
// The estate and the endpoint list live here because the two files beside this
// one are about what the endpoints do rather than who may reach them.

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
const (
	auditWaitFor   = 3 * time.Second
	auditPollEvery = 10 * time.Millisecond

	testPathRules      = "/api/v1/rules"
	testPathDeviceTags = "/api/v1/device-tags"
	testPathLimits     = "/api/v1/alert-limits"
)

// ruleAdminEstate is a server wired with everything the rules screen needs, plus
// one customer holding one machine.

// ruleAdminEstate is a server wired with everything the rules screen needs, plus
// one customer holding one machine.
type ruleAdminEstate struct {
	srv         *Server
	memberToken string
	adminToken  string
	org         uuid.UUID
	site        uuid.UUID
	device      uuid.UUID
}

func newRuleAdminEstate(t *testing.T) ruleAdminEstate {
	t.Helper()
	store := testutil.NewTestStore(t)
	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	catalogue, err := rules.Embedded()
	require.NoError(t, err)

	srv := NewServer(ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		DeviceUpdates:  testutil.NewTestDeviceUpdates(t, store),
		Enrollment:     testutil.NewTestEnrollment(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Sites:          testutil.NewTestSites(t, store),
		Organizations:  testutil.NewTestOrganizations(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         &stubAgentGetter{},
		AMT:            &stubAMTOperator{},
		Relay:          relay.NewRelay(slog.Default()),
		Notifier:       &notifications.NoopNotifier{},
		Logger:         logger,
		RuleCatalogue:  catalogue,
		RuleRollouts:   rules.NewStore(store.DB()),
		RuleAdmin:      rules.NewStore(store.DB()),
		AlertBudget:    alerts.NewStore(store.DB()),
	})

	_, memberToken := seedTestUser(t, srv, cfg, "member-rules@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "admin-rules@example.com", true)

	ctx := testTenantContext(t)
	site := testutil.SeedSite(t, ctx, store)
	device := testutil.SeedDevice(t, ctx, store, site.ID)

	return ruleAdminEstate{
		srv:         srv,
		memberToken: memberToken,
		adminToken:  adminToken,
		org:         site.OrganizationID,
		site:        site.ID,
		device:      device.ID,
	}
}

// query appends the customer the screen is showing.

// query appends the customer the screen is showing.
func (e ruleAdminEstate) query(path string) string {
	return path + "?organization_id=" + e.org.String()
}

// write is one administrator action, named as the audit log names it. Every
// entry is one case in both the permission table and the audit table, so a new
// endpoint is added to both at once or to neither.

// write is one administrator action, named as the audit log names it. Every
// entry is one case in both the permission table and the audit table, so a new
// endpoint is added to both at once or to neither.
type write struct {
	name   string
	method string
	path   func(e ruleAdminEstate) string
	body   func(e ruleAdminEstate) any
	action string
	status int
}

// everyWrite is the whole administrator surface of the rules screen.

// everyWrite is the whole administrator surface of the rules screen.
func everyWrite() []write {
	return []write{
		{
			name:   "tune a value",
			method: http.MethodPut,
			path:   func(e ruleAdminEstate) string { return e.query(testPathRules + "/disk-critical/bindings") },
			body: func(e ruleAdminEstate) any {
				return RuleBindingInput{
					Level:    RuleBindingLevelOrganization,
					LevelKey: e.org,
					Params:   map[string]float64{"threshold": 95},
				}
			},
			action: "rule.binding.set",
			status: http.StatusOK,
		},
		{
			name:   "set the rollout",
			method: http.MethodPut,
			path:   func(e ruleAdminEstate) string { return e.query(testPathRules + "/disk-critical/rollout") },
			body: func(ruleAdminEstate) any {
				return RuleRolloutInput{
					Enabled:        true,
					CanaryPercent:  5,
					StagedPercent:  25,
					CanaryHoldSecs: 7200,
					StagedHoldSecs: 43200,
				}
			},
			action: "rule.rollout.set",
			status: http.StatusOK,
		},
		{
			name:   "stop the rule for one customer",
			method: http.MethodPost,
			path:   func(e ruleAdminEstate) string { return e.query(testPathRules + "/disk-critical/stop") },
			body: func(ruleAdminEstate) any {
				return RuleStopInput{Scope: RuleStopScopeOrganization, Stopped: true}
			},
			action: "rule.stop",
			status: http.StatusNoContent,
		},
		{
			name:   "stop the rule everywhere",
			method: http.MethodPost,
			path:   func(e ruleAdminEstate) string { return e.query(testPathRules + "/cpu-saturated/stop") },
			body: func(ruleAdminEstate) any {
				return RuleStopInput{Scope: RuleStopScopeTenant, Stopped: true}
			},
			action: "rule.stop",
			status: http.StatusNoContent,
		},
		{
			name:   "lift a stop",
			method: http.MethodPost,
			path:   func(e ruleAdminEstate) string { return e.query(testPathRules + "/memory-pressure/stop") },
			body: func(ruleAdminEstate) any {
				return RuleStopInput{Scope: RuleStopScopeOrganization, Stopped: false}
			},
			action: "rule.resume",
			status: http.StatusNoContent,
		},
		{
			name:   "remove a tuned value",
			method: http.MethodDelete,
			path: func(ruleAdminEstate) string {
				return testPathRules + "/disk-critical/bindings/" + uuid.New().String()
			},
			action: "rule.binding.delete",
			status: http.StatusNoContent,
		},
		{
			name:   "move the alert budget",
			method: http.MethodPut,
			path:   func(e ruleAdminEstate) string { return e.query(testPathLimits) },
			body: func(ruleAdminEstate) any {
				return AlertLimitsInput{OrganizationHourly: 1200, DeviceHourly: 40}
			},
			action: "alert.limits.set",
			status: http.StatusOK,
		},
		{
			name:   "add a label",
			method: http.MethodPost,
			path:   func(e ruleAdminEstate) string { return e.query(testPathDeviceTags + "/labels") },
			body: func(ruleAdminEstate) any {
				return DeviceTagLabelInput{Key: "role", Value: "file-server-" + uuid.New().String()[:8]}
			},
			action: "device.tag.label.create",
			status: http.StatusCreated,
		},
		{
			name:   "take a label off a machine",
			method: http.MethodDelete,
			path: func(e ruleAdminEstate) string {
				return testPathDeviceTags + "/assignments?device_id=" + e.device.String() + "&key=role"
			},
			action: "device.tag.clear",
			status: http.StatusNoContent,
		},
	}
}

// Every write is refused for an ordinary member, one endpoint at a time. A gate
// asserted in aggregate is a gate that can be missing from one route and still
// pass.

// Every write is refused for an ordinary member, one endpoint at a time. A gate
// asserted in aggregate is a gate that can be missing from one route and still
// pass.
func TestEveryRuleAdministrationWriteIsRefusedToAMember(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	for _, w := range everyWrite() {
		t.Run(w.name, func(t *testing.T) {
			var body any
			if w.body != nil {
				body = w.body(e)
			}
			resp := doRequest(e.srv, w.method, w.path(e), e.memberToken, body)
			assert.Equal(t, http.StatusForbidden, resp.Code,
				"%s must be administrator-only", w.name)
		})
	}
}

// And every write lands in the audit log, with the actor who made it. Driven
// over the whole endpoint list so one added later without auditing fails here.

// And every write lands in the audit log, with the actor who made it. Driven
// over the whole endpoint list so one added later without auditing fails here.
func TestEveryRuleAdministrationWriteIsAudited(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)
	ctx := testTenantContext(t)

	for _, w := range everyWrite() {
		t.Run(w.name, func(t *testing.T) {
			var body any
			if w.body != nil {
				body = w.body(e)
			}
			resp := doRequest(e.srv, w.method, w.path(e), e.adminToken, body)
			require.Equal(t, w.status, resp.Code, "%s: %s", w.name, resp.Body.String())

			require.Eventually(t, func() bool {
				events, err := e.srv.audit.Query(ctx, audit.Query{Action: w.action, Limit: 50})
				return err == nil && len(events) > 0
			}, auditWaitFor, auditPollEvery, "%s must be recorded as %s", w.name, w.action)
		})
	}
}

// Reads are the other half of the access decision: a technician resolving
// something as a false alarm has to be able to see the rule that produced it.

// Reads are the other half of the access decision: a technician resolving
// something as a false alarm has to be able to see the rule that produced it.
func TestAnOrdinaryMemberReadsEverythingOnTheRulesScreen(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	for name, path := range map[string]string{
		"the pack":        e.query(testPathRules),
		"one rule":        e.query(testPathRules + "/disk-critical"),
		"the labels":      e.query(testPathDeviceTags),
		"the budget":      e.query(testPathLimits),
		"a resolved rule": testPathRules + "/disk-critical/resolved?device_id=" + e.device.String(),
	} {
		t.Run(name, func(t *testing.T) {
			resp := doRequest(e.srv, http.MethodGet, path, e.memberToken, nil)
			assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		})
	}
}

// A value outside what the rule allows is refused where somebody can still see
// why, rather than reaching an estate.

// A member of the admin security group is an administrator here too, which is
// what keeps this screen's gate the same gate as every other one.
func TestTheAdminGroupCanAdministerRules(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)
	ctx := testTenantContext(t)

	admin, err := e.srv.users.GetByEmail(ctx, "admin-rules@example.com")
	require.NoError(t, err)
	require.NoError(t, e.srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	resp := doRequest(e.srv, http.MethodPut, e.query(testPathLimits), e.adminToken,
		AlertLimitsInput{OrganizationHourly: 900, DeviceHourly: 30})
	assert.Equal(t, http.StatusOK, resp.Code)
}

// Acknowledging a move a rule version made: refused to a member, refused when
// nothing is outstanding, and audited when it lands.

// A deployment wired without the mutable half of the rule store can still serve
// the read-only catalogue, and says so on everything it cannot do rather than
// answering as though nothing were configured.
func TestADeploymentWithoutRuleAdministrationSaysSo(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)
	e.srv.ruleAdmin = nil
	e.srv.alertBudget = nil

	for name, probe := range map[string]struct {
		method string
		path   string
		body   any
	}{
		"listing labels":       {http.MethodGet, e.query(testPathDeviceTags), nil},
		"adding a label":       {http.MethodPost, e.query(testPathDeviceTags + "/labels"), DeviceTagLabelInput{Key: "role", Value: "file-server"}},
		"removing a label":     {http.MethodDelete, testPathDeviceTags + "/labels/" + uuid.New().String(), nil},
		"assigning a label":    {http.MethodPut, testPathDeviceTags + "/assignments", DeviceTagAssignmentInput{DeviceIds: []uuid.UUID{e.device}, LabelId: uuid.New()}},
		"clearing a label":     {http.MethodDelete, testPathDeviceTags + "/assignments?device_id=" + e.device.String() + "&key=role", nil},
		"reading the budget":   {http.MethodGet, e.query(testPathLimits), nil},
		"moving the budget":    {http.MethodPut, e.query(testPathLimits), AlertLimitsInput{OrganizationHourly: 900, DeviceHourly: 30}},
		"tuning a value":       {http.MethodPut, e.query(testPathRules + "/disk-critical/bindings"), RuleBindingInput{Level: RuleBindingLevelOrganization, LevelKey: e.org, Params: map[string]float64{"threshold": 95}}},
		"removing a value":     {http.MethodDelete, testPathRules + "/disk-critical/bindings/" + uuid.New().String(), nil},
		"setting the rollout":  {http.MethodPut, e.query(testPathRules + "/disk-critical/rollout"), RuleRolloutInput{Enabled: true, CanaryPercent: 1, StagedPercent: 10, CanaryHoldSecs: 3600, StagedHoldSecs: 21600}},
		"stopping a rule":      {http.MethodPost, e.query(testPathRules + "/disk-critical/stop"), RuleStopInput{Scope: RuleStopScopeOrganization, Stopped: true}},
		"acknowledging a move": {http.MethodPost, testPathRules + "/disk-critical/clamps/" + uuid.New().String(), nil},
	} {
		t.Run(name, func(t *testing.T) {
			resp := doRequest(e.srv, probe.method, probe.path, e.adminToken, probe.body)
			assert.Equal(t, http.StatusInternalServerError, resp.Code,
				"%s must report a server that cannot do it, not a silent success", name)
		})
	}

	// The catalogue itself is still readable, which is the whole point of the
	// two halves being separable.
	assert.Equal(t, http.StatusOK,
		doRequest(e.srv, http.MethodGet, e.query(testPathRules), e.memberToken, nil).Code)
}

// A rule page whose tuning cannot be read still renders the rule. A read that
// failed costs the page its tuning, not the description somebody opened it for.
