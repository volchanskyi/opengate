package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/app"
)

// internalPaths are the unauthenticated process-internal routes the cluster-only listener serves.
var internalPaths = []string{
	"/metrics",
	"/debug/pprof/",
	"/debug/pprof/heap",
	"/debug/pprof/goroutine",
	"/debug/pprof/cmdline",
	"/debug/pprof/symbol",
	"/debug/pprof/trace",
}

func TestInternalListenerIsTheOnlyWayToTheProcessInternals(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)
	require.NotNil(t, assembly.Internal, "the assembly must build the cluster-only listener")
	require.NotNil(t, assembly.Internal.Handler)

	for _, path := range internalPaths {
		t.Run(path, func(t *testing.T) {
			internal := httptest.NewRecorder()
			assembly.Internal.Handler.ServeHTTP(internal, httptest.NewRequest(http.MethodGet, path, nil))
			assert.Equal(t, http.StatusOK, internal.Code,
				"%s must answer on the internal listener", path)

			public := httptest.NewRecorder()
			assembly.API.ServeHTTP(public, httptest.NewRequest(http.MethodGet, path, nil))
			assert.Equal(t, http.StatusNotFound, public.Code,
				"%s must not be reachable on the listener the ingress publishes", path)
		})
	}
}

func TestInternalListenerServesTheExposition(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	assembly.Internal.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	page := rec.Body.String()
	for _, family := range []string{
		"go_goroutines",
		"process_resident_memory_bytes",
		"process_open_fds",
		"process_start_time_seconds",
	} {
		assert.True(t, strings.Contains(page, family),
			"the exposition must carry %s — a load run reads its target's health off this page", family)
	}
}

func TestInternalListenerAddressComesFromConfiguration(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(t)
	cfg.InternalListen = "127.0.0.1:18099"

	assembly, err := app.Build(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:18099", assembly.Internal.Addr)
}

func TestPublicListenerKeepsTheLivenessProbe(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	assembly.API.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code, "/healthz must stay on the listener the kubelet probes")
}

func TestTheExpositionCarriesWhatTheProcessIsHolding(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	assembly.Internal.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	page := rec.Body.String()
	for _, series := range []string{
		"opengate_agents_connected 0",
		"opengate_relay_active_sessions 0",
		"opengate_relay_sessions_started_total 0",
		"opengate_mps_connected_devices 0",
	} {
		assert.True(t, strings.Contains(page, series),
			"the exposition must carry %q — a load run reads the fleet its target is holding off this page", series)
	}
}
