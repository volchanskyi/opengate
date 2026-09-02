package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"log/slog"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// quietLogger keeps the assembly's boot lines out of the test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// baseConfig is a complete, valid configuration: every acceptance of Build in
// this file starts from it and removes exactly one thing.
func baseConfig(t *testing.T) app.Config {
	t.Helper()
	return app.Config{
		Store:     testutil.NewTestStore(t),
		DataDir:   t.TempDir(),
		JWTSecret: "app-package-test-secret-32-bytes!",
		Logger:    quietLogger(),
	}
}

// TestBuildNamesTheDependencyItIsMissing is the harness-failure contract: a
// configuration short of something required fails naming it, rather than
// assembling a server that answers 500 on the routes that needed it.
func TestBuildNamesTheDependencyItIsMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*app.Config)
		wantErr string
	}{
		{"no store", func(c *app.Config) { c.Store = nil }, "Store"},
		{"no data dir", func(c *app.Config) { c.DataDir = "" }, "DataDir"},
		{"no jwt secret", func(c *app.Config) { c.JWTSecret = "" }, "JWTSecret"},
		{"short jwt secret", func(c *app.Config) { c.JWTSecret = "too-short" }, "JWTSecret"},
		{"no logger", func(c *app.Config) { c.Logger = nil }, "Logger"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseConfig(t)
			tc.mutate(&cfg)

			assembly, err := app.Build(context.Background(), cfg)

			require.Error(t, err)
			assert.Nil(t, assembly)
			assert.Contains(t, err.Error(), tc.wantErr,
				"the error must name the missing dependency so the operator knows what to wire")
		})
	}
}

// TestBuildAssemblesTheWholeProduct proves one call wires every port the
// process needs, and that the assembled API server actually serves.
func TestBuildAssemblesTheWholeProduct(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	require.NotNil(t, assembly.API)
	require.NotNil(t, assembly.Agents)
	require.NotNil(t, assembly.MPS)
	require.NotNil(t, assembly.AMT)
	require.NotNil(t, assembly.Relay)
	require.NotNil(t, assembly.Signaling)
	require.NotNil(t, assembly.Metrics)
	require.NotNil(t, assembly.MetricsRegistry)
	require.NotNil(t, assembly.Cert)
	require.NotNil(t, assembly.Alerts)
	require.NotNil(t, assembly.Rules)
	require.NotNil(t, assembly.Sessions)
	require.NotNil(t, assembly.JWT)
	require.NotNil(t, assembly.Enrollment)
	require.NotNil(t, assembly.Devices)
	require.NotNil(t, assembly.Sites)
	require.NotNil(t, assembly.Organizations)
	require.NotNil(t, assembly.Users)
	require.NotNil(t, assembly.SecurityGroups)
	require.NotNil(t, assembly.SigningKeys)
	require.NotNil(t, assembly.Manifests)
	require.NotNil(t, assembly.Store)

	rec := httptest.NewRecorder()
	assembly.API.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code, "the assembled server serves its own health route")
}

// TestBuildTwiceSharesNoMutableState is what lets every acceptance test run in
// parallel: two products built in one process must not be able to see each
// other's connections, sessions or counters.
func TestBuildTwiceSharesNoMutableState(t *testing.T) {
	t.Parallel()

	first, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)
	second, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	assert.NotSame(t, first.Relay, second.Relay)
	assert.NotSame(t, first.Agents, second.Agents)
	assert.NotSame(t, first.Metrics, second.Metrics)
	assert.NotSame(t, first.MetricsRegistry, second.MetricsRegistry)
	assert.NotSame(t, first.Cert, second.Cert)
	assert.NotSame(t, first.API, second.API)
	assert.NotSame(t, first.Internal, second.Internal)
	assert.NotSame(t, first.Store, second.Store)

	// Signaling is not in that list because it is a value: the ICE servers a
	// browser is told to try are configuration, and two products holding equal
	// copies of it is the point rather than a leak.
	assert.Equal(t, first.Signaling, second.Signaling)
}

// TestBuildWithoutNumericTelemetryLeavesPurgingOff pins the documented
// fallback: without a metrics store there is no series to purge, so device
// deletion is the plain Postgres delete and no orchestrator is wired.
func TestBuildWithoutNumericTelemetryLeavesPurgingOff(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	assert.Nil(t, assembly.Purger)
	assert.Nil(t, assembly.PurgeJobs)
	assert.Nil(t, assembly.Reconciler)
}

// TestBuildWithNumericTelemetryWiresPurging is the other half: given a metrics
// store, the erasure orchestrator and its reconciliation sweep exist.
func TestBuildWithNumericTelemetryWiresPurging(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(t)
	cfg.VictoriaMetricsURL = testvm.BaseURL(t)

	assembly, err := app.Build(context.Background(), cfg)
	require.NoError(t, err)

	assert.NotNil(t, assembly.Purger)
	assert.NotNil(t, assembly.PurgeJobs)
	assert.NotNil(t, assembly.Reconciler)
}

// TestBuildRejectsAnUnusableDataDir proves the assembly refuses rather than
// half-building when the certificate material cannot be written.
func TestBuildRejectsAnUnusableDataDir(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(t)
	blocked := cfg.DataDir + "/not-a-directory"
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o600))
	cfg.DataDir = blocked

	assembly, err := app.Build(context.Background(), cfg)

	require.Error(t, err)
	assert.Nil(t, assembly)
	assert.True(t, strings.Contains(err.Error(), "data dir") || strings.Contains(err.Error(), "certificate"),
		"got %q", err.Error())
}

// TestAgentControlGetterConvertsAMissingAgentToANilInterface pins the bridge
// the composition root owns: the handlers test `ac == nil`, and a typed-nil
// *AgentConn would defeat that check.
func TestAgentControlGetterConvertsAMissingAgentToANilInterface(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	assert.Nil(t, assembly.AgentControl.GetAgent(uuid.New()))
	assert.Empty(t, assembly.AgentControl.ListConnectedAgents())
}
