package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// TestServer_MetricsWiring verifies that NewServer wires the metrics
// middleware when Metrics is non-nil, and that the exposition it feeds is not
// reachable on this listener. Without the first half the CONDITIONALS_NEGATION
// mutant on that `!= nil` check survives: API tests that pass nil metrics never
// exercise the registered branch. The second half is the boundary — the
// exposition renders process internals to anyone who asks, and everything this
// router serves is published by one catch-all ingress rule, so it belongs to
// the cluster-only listener the composition root builds.
func TestServer_MetricsWiring(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	cfg := &auth.JWTConfig{
		Secret:   "test-secret-key-at-least-32-bytes!",
		Issuer:   "opengate-test",
		Duration: testJWTConfig().Duration,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	registry := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(registry)

	srv := NewServer(ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Sites:          testutil.NewTestSites(t, store),
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
		Metrics:        m,
	})

	// 1) The exposition is absent from the listener the ingress publishes.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code,
		"/metrics belongs to the cluster-only listener, not to the public router")

	// 2) HTTPMiddleware is wired when Metrics != nil (kills the api.go
	// conditional mutation). Issue a request that hits a real route, then read
	// the registry the middleware writes to — the same registry the internal
	// listener renders. If the middleware were not registered, the counter
	// would have zero series.
	hreq := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	hw := httptest.NewRecorder()
	srv.ServeHTTP(hw, hreq)

	scrape := gatherText(t, registry)
	assert.True(t,
		strings.Contains(scrape, `opengate_http_requests_total{method="GET"`),
		"metrics middleware should record an HTTP request series for GET, got:\n%s", scrape)
}

// gatherText renders a registry the way the internal listener does, so a test
// in this package can assert what the middleware wrote without the endpoint
// that renders it being on this package's router.
func gatherText(t *testing.T, registry *prometheus.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}
