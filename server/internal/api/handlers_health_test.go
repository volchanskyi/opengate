package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/relay"
)

// degradedRegistry embeds an in-process registry (inheriting every method) and
// overrides only Ping to report the backing store as unreachable — the
// Redis-loss condition the readiness probe must drain on.
type degradedRegistry struct {
	*relay.InProcessRegistry
}

func (degradedRegistry) Ping(context.Context) error {
	return errors.New("registry unreachable")
}

// degradedRelayServer builds a test server whose relay's registry reports itself
// unreachable, simulating Redis loss for the health probes.
func degradedRelayServer(t *testing.T) *httptest.Server {
	t.Helper()
	r := relay.NewRelay(slog.Default(), relay.WithRegistry(degradedRegistry{relay.NewInProcessRegistry()}, "test"))
	ts, _, _ := newRelayTestServerWith(t, r)
	return ts
}

// getStatus issues a GET and returns the HTTP status code.
func getStatus(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestHealthz_LivenessIsDependencyFree asserts the liveness route returns 200
// regardless of backing-store state — a Redis/Postgres blip must not restart
// the pod.
func TestHealthz_LivenessIsDependencyFree(t *testing.T) {
	ts := degradedRelayServer(t)
	assert.Equal(t, http.StatusOK, getStatus(t, ts.URL+"/healthz"))
}

// TestHealth_ReadyWhenRegistryHealthy asserts the readiness probe reports 200
// when Postgres and the registry are both reachable.
func TestHealth_ReadyWhenRegistryHealthy(t *testing.T) {
	ts, _, _ := newRelayTestServer(t) // default relay = in-process registry, always healthy
	assert.Equal(t, http.StatusOK, getStatus(t, ts.URL+"/api/v1/health"))
}

// TestHealth_DrainsWhenRegistryDown asserts the readiness probe reports 503 when
// the session registry (Redis) is unreachable, so k8s drains the pod while
// liveness keeps it running.
func TestHealth_DrainsWhenRegistryDown(t *testing.T) {
	ts := degradedRelayServer(t)
	assert.Equal(t, http.StatusServiceUnavailable, getStatus(t, ts.URL+"/api/v1/health"))
}
