package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// The investigation surface, driven through HTTP.
//
// Two things are being proved here and they fail differently. Tenancy is the
// wall: a caller from another tenant must not be able to tell an incident they
// may not see from one that does not exist, on any route, including with an id
// they guessed. Narrowing to a customer is the quieter one — both customers are
// inside one tenant, so nothing is refused and a wrong query simply shows
// Contoso's estate to somebody looking at Fabrikam's.
//
// The rest is what a technician can actually do: page a queue that is moving,
// move an incident only the way the lifecycle allows, and read the frozen
// evidence behind one alert without the list ever carrying it.

// investigations is a server wired for the incident routes, with a seeded
// customer, machine and technician.
type investigations struct {
	srv    *Server
	store  *db.PostgresStore
	alerts *alerts.Store
	cfg    *auth.JWTConfig
	ctx    context.Context
	token  string
	user   *auth.User
	org    uuid.UUID
	site   uuid.UUID
	device uuid.UUID
	now    time.Time
}

// stubRuleCoverage answers the coverage split without a connected fleet, which
// is what a handler test can have.
type stubRuleCoverage struct {
	counts map[string]agentapi.RuleCoverageCounts
}

func (s stubRuleCoverage) RuleCoverage(
	_ context.Context, _ uuid.UUID, fleetSize int,
) map[string]agentapi.RuleCoverageCounts {
	if s.counts != nil {
		return s.counts
	}
	// Everything unknown is the honest answer for an install nothing has
	// reported into, and it still adds up to the fleet.
	return map[string]agentapi.RuleCoverageCounts{
		"disk-critical": {Unknown: fleetSize},
	}
}

// newInvestigations builds the server and the estate every case below starts
// from.
func newInvestigations(t *testing.T, coverage RuleCoverageReader) investigations {
	t.Helper()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(t.Context(), true)
	site := testutil.SeedSite(t, ctx, store)
	device := testutil.SeedDevice(t, ctx, store, site.ID)

	catalogue, err := rules.Embedded()
	require.NoError(t, err)

	alertStore := alerts.NewStore(store.DB())
	// Event time is stated by the fixture; receipt time is the store's own
	// clock, which stays real so the customer's rolling hourly budget behaves
	// the way it does in production.
	now := time.Now().UTC().Truncate(time.Second)

	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Sites:          testutil.NewTestSites(t, store),
		Organizations:  testutil.NewTestOrganizations(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         &stubAgentGetter{},
		AMT:            &stubAMTOperator{},
		Relay:          relay.NewRelay(slog.Default()),
		Notifier:       &notifications.NoopNotifier{},
		Investigations: alertStore,
		RuleCatalogue:  catalogue,
		RuleRollouts:   rules.NewStore(store.DB()),
		RuleCoverage:   coverage,
		Logger:         logger,
	})

	user, token := seedTestUser(t, srv, cfg, "tech@example.com", false)
	return investigations{
		srv: srv, store: store, alerts: alertStore, cfg: cfg, ctx: ctx,
		token: token, user: user, org: site.OrganizationID, site: site.ID,
		device: device.ID, now: now,
	}
}

// open files one alert and returns the incident it opened, which is how every
// case gets a room to work with through the same path production uses.
func (e investigations) open(t *testing.T, severity alerts.Severity, evidence []byte) (uuid.UUID, uuid.UUID) {
	t.Helper()
	alert := alerts.Alert{
		ID: uuid.New(), OrganizationID: e.org, DeviceID: e.device,
		RuleID: "disk-critical", RuleVersion: 1, Severity: severity,
		Metric: "disk.used_percent", WindowStart: e.now, WindowEnd: e.now, ObservedAt: e.now,
	}
	if len(evidence) > 0 {
		alert.Evidence, alert.EvidenceCodec = evidence, protocol.EvidenceCodec
	}
	grouping := alerts.Grouping{Scope: alerts.ScopeDevice, Window: 15 * time.Minute}
	outcome, err := e.alerts.Record(e.ctx, alert, grouping)
	require.NoError(t, err)
	require.Equal(t, alerts.Stored, outcome)

	incident, found, err := e.alerts.OpenIncident(e.ctx, e.org, "disk-critical", alerts.ScopeDevice, e.device)
	require.NoError(t, err)
	require.True(t, found)
	return incident.ID, alert.ID
}

// stranger is a token for a caller in a tenant of their own, which is what a
// crafted id has to be tried with.
func (e investigations) stranger(t *testing.T) string {
	t.Helper()
	token, err := e.cfg.GenerateToken(uuid.New(), "elsewhere@example.com", false, uuid.New())
	require.NoError(t, err)
	return token
}

// TestInvestigationRoutesStopAtTheTenantWall walks every route with an incident
// from another tenant. Route by route, because a single missed guard is the
// whole class of defect this is here to close, and one route covered by another
// route's test proves nothing about the one that was forgotten.
func TestInvestigationRoutesStopAtTheTenantWall(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, alert := e.open(t, alerts.SeverityCritical, nil)
	outsider := e.stranger(t)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"the incident", http.MethodGet, "/api/v1/investigations/" + incident.String(), nil},
		{"its evidence", http.MethodGet,
			"/api/v1/investigations/" + incident.String() + "/alerts/" + alert.String() + "/evidence", nil},
		{"a status change", http.MethodPost, "/api/v1/investigations/" + incident.String() + "/status",
			SetIncidentStatusRequest{Status: Acknowledged}},
		{"an assignment", http.MethodPost, "/api/v1/investigations/" + incident.String() + "/assignee",
			SetIncidentAssigneeRequest{}},
		{"a comment", http.MethodPost, "/api/v1/investigations/" + incident.String() + "/comments",
			AddIncidentCommentRequest{Body: "mine now"}},
		{"the machine's strip", http.MethodGet, "/api/v1/devices/" + e.device.String() + "/incidents", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := doRequest(e.srv, tc.method, tc.path, outsider, tc.body)
			assert.Equal(t, http.StatusNotFound, w.Code,
				"%s must be indistinguishable from one that does not exist: %s", tc.name, w.Body.String())
		})
	}

	// The queue itself is not a 404 — it is a queue, and another tenant's is
	// empty rather than forbidden.
	w := doRequest(e.srv, http.MethodGet, "/api/v1/investigations", outsider, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var page IncidentPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	assert.Empty(t, page.Items)
}

// TestInvestigationRoutesStopAtTheCustomer is the quieter leak. Both customers
// sit inside one tenant, so the wall is not breached and nothing is refused —
// only the query decides, and getting it wrong shows one customer's estate to
// somebody looking at another's.
func TestInvestigationRoutesStopAtTheCustomer(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, alert := e.open(t, alerts.SeverityCritical, nil)
	fabrikam := testutil.SeedOrganization(t, e.ctx, e.store, "Fabrikam")
	elsewhere := "?organization_id=" + fabrikam.String()

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"the incident", http.MethodGet, "/api/v1/investigations/" + incident.String() + elsewhere, nil},
		{"its evidence", http.MethodGet,
			"/api/v1/investigations/" + incident.String() + "/alerts/" + alert.String() + "/evidence" + elsewhere, nil},
		{"a status change", http.MethodPost,
			"/api/v1/investigations/" + incident.String() + "/status" + elsewhere,
			SetIncidentStatusRequest{Status: Acknowledged}},
		{"an assignment", http.MethodPost,
			"/api/v1/investigations/" + incident.String() + "/assignee" + elsewhere,
			SetIncidentAssigneeRequest{}},
		{"a comment", http.MethodPost,
			"/api/v1/investigations/" + incident.String() + "/comments" + elsewhere,
			AddIncidentCommentRequest{Body: "mine now"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := doRequest(e.srv, tc.method, tc.path, e.token, tc.body)
			assert.Equal(t, http.StatusNotFound, w.Code,
				"%s while looking at another customer: %s", tc.name, w.Body.String())
		})
	}

	w := doRequest(e.srv, http.MethodGet, "/api/v1/investigations"+elsewhere, e.token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var page IncidentPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	assert.Empty(t, page.Items, "another customer's queue holds none of this customer's rooms")
}

// TestTriageQueuePagesByCursor. The queue is read while it is being written to,
// so the page says where it ended and the next one starts there.
func TestTriageQueuePagesByCursor(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	e.open(t, alerts.SeverityCritical, nil)
	e.seedRooms(t, 2)

	w := doRequest(e.srv, http.MethodGet, "/api/v1/investigations?limit=2", e.token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var first IncidentPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.Len(t, first.Items, 2)
	require.NotNil(t, first.NextCursor)

	w = doRequest(e.srv, http.MethodGet,
		"/api/v1/investigations?limit=2&cursor="+*first.NextCursor, e.token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var second IncidentPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &second))
	require.Len(t, second.Items, 1)
	assert.Nil(t, second.NextCursor, "the last page offers no cursor")
	assert.NotEqual(t, first.Items[0].Id, second.Items[0].Id)

	w = doRequest(e.srv, http.MethodGet, "/api/v1/investigations?cursor=not-a-position", e.token, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, "a cursor that names nothing is refused, not ignored")
}

// TestTriageQueueNeverCarriesEvidence is the bound that keeps the queue cheap.
// Evidence is tens of kilobytes per alert; a queue that embedded it would drag
// megabytes to render a list nobody has clicked into.
func TestTriageQueueNeverCarriesEvidence(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, _ := e.open(t, alerts.SeverityCritical, encodedEvidence(t, sampleEvidence()))

	w := doRequest(e.srv, http.MethodGet, "/api/v1/investigations", e.token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "log_samples")
	assert.NotContains(t, w.Body.String(), "\"evidence\"")

	// The detail says what evidence there is and what fetching it costs, and
	// still does not carry it.
	w = doRequest(e.srv, http.MethodGet, "/api/v1/investigations/"+incident.String(), e.token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "log_samples")

	var detail IncidentDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.NotEmpty(t, detail.Alerts)
	assert.Equal(t, protocol.EvidenceCodec, *detail.Alerts[0].EvidenceCodec)
	assert.Positive(t, detail.Alerts[0].EvidenceBytes)
}

// TestDeviceStripSharesTheQueue. The machine's incidents are the same read
// narrowed to it, including the customer-wide rooms it is one of forty machines
// in — not a second list implementation that can drift from the first.
func TestDeviceStripSharesTheQueue(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, _ := e.open(t, alerts.SeverityCritical, nil)
	e.seedRooms(t, 2)

	w := doRequest(e.srv, http.MethodGet, "/api/v1/devices/"+e.device.String()+"/incidents", e.token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var page IncidentPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Items, 1, "only the rooms this machine raised an alert into")
	assert.Equal(t, incident, page.Items[0].Id)

	w = doRequest(e.srv, http.MethodGet, "/api/v1/devices/"+uuid.New().String()+"/incidents", e.token, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, "a machine outside the tenant has no strip")
}

func (e investigations) seedRooms(t *testing.T, n int) {
	t.Helper()
	// Another machine's alerts, so the strip narrowing to one machine is proved
	// by what it leaves out as well as by what it returns.
	elsewhere := testutil.SeedDevice(t, e.ctx, e.store, e.site)
	for i := range n {
		alert := alerts.Alert{
			ID: uuid.New(), OrganizationID: e.org, DeviceID: elsewhere.ID,
			RuleID: []string{"cpu-saturated", "memory-exhausted"}[i%2], RuleVersion: 1,
			Severity:    alerts.SeverityWarning,
			WindowStart: e.now.Add(-time.Duration(i+1) * time.Hour),
			WindowEnd:   e.now.Add(-time.Duration(i+1) * time.Hour),
			ObservedAt:  e.now.Add(-time.Duration(i+1) * time.Hour),
		}
		outcome, err := e.alerts.Record(e.ctx, alert,
			alerts.Grouping{Scope: alerts.ScopeOrganization, Window: 30 * time.Minute})
		require.NoError(t, err)
		require.Equal(t, alerts.Stored, outcome)
	}
}

// rewriteEvidence replaces one alert's stored blob, which is how a case builds
// the row a future codec would leave behind.
func (e investigations) rewriteEvidence(t *testing.T, alertID uuid.UUID, blob []byte, codec string) {
	t.Helper()
	_, err := e.store.DB().ExecContext(e.ctx,
		`UPDATE alerts SET evidence = $2, evidence_codec = $3 WHERE id = $1`, alertID, blob, codec)
	require.NoError(t, err)
}
