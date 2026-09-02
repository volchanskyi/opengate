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

// internalPaths are the routes that belong to the cluster and to nobody else:
// the exposition a scraper reads, and the profiler an operator attaches to a
// pod that is misbehaving. Both render process internals, and neither has any
// authentication in front of it, so the boundary is the listener rather than a
// rule on the edge.
var internalPaths = []string{
	"/metrics",
	"/debug/pprof/",
	"/debug/pprof/heap",
	"/debug/pprof/goroutine",
	"/debug/pprof/cmdline",
	"/debug/pprof/symbol",
	"/debug/pprof/trace",
}

// TestInternalListenerIsTheOnlyWayToTheProcessInternals asserts the boundary as
// two handlers rather than as one: every internal path answers on the internal
// listener and is absent from the public one. Stating it on a single handler
// would prove only that a route exists somewhere, which is the belief that let
// two comments assert a boundary the ingress had stopped providing.
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

// TestInternalListenerServesTheExposition proves the moved endpoint still
// renders the registry the process instruments itself with — the four families
// this incident was diagnosed from live on that page.
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

// TestInternalListenerAddressComesFromConfiguration keeps the port the process
// binds and the port the chart, the scrape job and the harness name in one
// place: a listener that ignored its configured address would answer every test
// above and still be unreachable in the cluster.
func TestInternalListenerAddressComesFromConfiguration(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(t)
	cfg.InternalListen = "127.0.0.1:18099"

	assembly, err := app.Build(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:18099", assembly.Internal.Addr)
}

// TestPublicListenerKeepsTheLivenessProbe fixes the one route that must not
// move: the kubelet probes the container's published port, so /healthz staying
// public is what keeps the pod restartable.
func TestPublicListenerKeepsTheLivenessProbe(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	assembly.API.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code, "/healthz must stay on the listener the kubelet probes")
}
