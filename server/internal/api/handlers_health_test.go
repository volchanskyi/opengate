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

// degradedRegistry overrides Ping on an in-process registry to report the store unreachable.
type degradedRegistry struct {
	*relay.InProcessRegistry
}

func (degradedRegistry) Ping(context.Context) error {
	return errors.New("registry unreachable")
}

func degradedRelayServer(t *testing.T) *httptest.Server {
	t.Helper()
	r := relay.NewRelay(slog.Default(), relay.WithRegistry(degradedRegistry{relay.NewInProcessRegistry()}, "test"))
	ts, _, _ := newRelayTestServerWith(t, r)
	return ts
}

func getStatus(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestHealthz_LivenessIsDependencyFree(t *testing.T) {
	ts := degradedRelayServer(t)
	assert.Equal(t, http.StatusOK, getStatus(t, ts.URL+"/healthz"))
}

func TestHealth_ReadyWhenRegistryHealthy(t *testing.T) {
	ts, _, _ := newRelayTestServer(t)
	assert.Equal(t, http.StatusOK, getStatus(t, ts.URL+"/api/v1/health"))
}

func TestHealth_DrainsWhenRegistryDown(t *testing.T) {
	ts := degradedRelayServer(t)
	assert.Equal(t, http.StatusServiceUnavailable, getStatus(t, ts.URL+"/api/v1/health"))
}
