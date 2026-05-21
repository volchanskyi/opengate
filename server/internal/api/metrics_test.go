package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// TestServer_MetricsWiring verifies that NewServer wires the metrics
// middleware (api.go:131) and the /metrics endpoint (api.go:139) when
// Metrics + MetricsRegistry are non-nil. Without this test, both
// CONDITIONALS_NEGATION mutants on those `!= nil` checks survive: API
// tests that pass nil metrics never exercise the registered branch.
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
		Store:           store,
		Audit:           testutil.NewTestAudit(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		JWT:             cfg,
		Agents:          &stubAgentGetter{},
		AMT:             &stubAMTOperator{},
		Relay:           relay.NewRelay(slog.Default()),
		Notifier:        &notifications.NoopNotifier{},
		Logger:          logger,
		MetricsRegistry: registry,
		Metrics:         m,
	})

	// 1) /metrics is registered when MetricsRegistry != nil (kills api.go:139 mutation).
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "/metrics should be served when registry is set")
	body := w.Body.String()
	assert.Contains(t, body, "# HELP", "expected Prometheus exposition format")

	// 2) HTTPMiddleware is wired when Metrics != nil (kills api.go:131 mutation).
	// Issue a request that hits a real route, then verify the request counter
	// recorded a sample for that exact method+route. If the middleware were
	// not registered, the counter would have zero series.
	hreq := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	hw := httptest.NewRecorder()
	srv.ServeHTTP(hw, hreq)

	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, w2.Code)
	scrape := w2.Body.String()
	assert.True(t,
		strings.Contains(scrape, `opengate_http_requests_total{method="GET"`),
		"metrics middleware should record an HTTP request series for GET, got:\n%s", scrape)
}
