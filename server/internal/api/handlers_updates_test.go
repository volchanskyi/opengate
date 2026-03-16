package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

func newTestServerWithUpdater(t *testing.T) (*Server, string, string) {
	t.Helper()
	store := testutil.NewTestStore(t)
	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir := t.TempDir()
	signing, err := updater.LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)
	manifests := updater.NewManifestStore(dir)

	srv := NewServer(ServerConfig{
		Store:     store,
		JWT:       cfg,
		Agents:    &stubAgentGetter{},
		AMT:       &stubAMTOperator{},
		Relay:     relay.NewRelay(),
		Notifier:  &notifications.NoopNotifier{},
		Signing:   signing,
		Manifests: manifests,
		Logger:    logger,
	})

	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)
	_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

	return srv, adminToken, userToken
}

func TestListUpdateManifests_Empty(t *testing.T) {
	srv, adminToken, _ := newTestServerWithUpdater(t)

	w := doRequest(srv, http.MethodGet, "/api/v1/updates/manifests", adminToken, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var manifests []AgentManifest
	require.NoError(t, json.NewDecoder(w.Body).Decode(&manifests))
	assert.Empty(t, manifests)
}

func TestPublishUpdate_Success(t *testing.T) {
	srv, adminToken, _ := newTestServerWithUpdater(t)

	body := PublishUpdateRequest{
		Version: "1.0.0",
		Os:      "linux",
		Arch:    "amd64",
		Url:     "https://github.com/example/releases/download/v1.0.0/mesh-agent-linux-amd64",
		Sha256:  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
	}

	w := doRequest(srv, http.MethodPost, "/api/v1/updates/manifests", adminToken, body)
	assert.Equal(t, http.StatusOK, w.Code)

	var manifest AgentManifest
	require.NoError(t, json.NewDecoder(w.Body).Decode(&manifest))
	assert.Equal(t, "1.0.0", manifest.Version)
	assert.Equal(t, "linux", manifest.Os)
	assert.Equal(t, "amd64", manifest.Arch)
	assert.NotEmpty(t, manifest.Signature)
}

func TestPublishUpdate_NonAdmin(t *testing.T) {
	srv, _, userToken := newTestServerWithUpdater(t)

	body := PublishUpdateRequest{
		Version: "1.0.0",
		Os:      "linux",
		Arch:    "amd64",
		Url:     "https://example.com/agent",
		Sha256:  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
	}

	w := doRequest(srv, http.MethodPost, "/api/v1/updates/manifests", userToken, body)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestListUpdateManifests_AfterPublish(t *testing.T) {
	srv, adminToken, _ := newTestServerWithUpdater(t)

	body := PublishUpdateRequest{
		Version: "1.0.0",
		Os:      "linux",
		Arch:    "amd64",
		Url:     "https://example.com/agent",
		Sha256:  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
	}
	w := doRequest(srv, http.MethodPost, "/api/v1/updates/manifests", adminToken, body)
	require.Equal(t, http.StatusOK, w.Code)

	w = doRequest(srv, http.MethodGet, "/api/v1/updates/manifests", adminToken, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var manifests []AgentManifest
	require.NoError(t, json.NewDecoder(w.Body).Decode(&manifests))
	assert.Len(t, manifests, 1)
	assert.Equal(t, "1.0.0", manifests[0].Version)
}

func TestPushUpdate_NoManifest(t *testing.T) {
	srv, adminToken, _ := newTestServerWithUpdater(t)

	body := PushUpdateRequest{
		Version: "1.0.0",
		Os:      "linux",
		Arch:    "amd64",
	}

	w := doRequest(srv, http.MethodPost, "/api/v1/updates/push", adminToken, body)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPushUpdate_NonAdmin(t *testing.T) {
	srv, _, userToken := newTestServerWithUpdater(t)

	body := PushUpdateRequest{
		Version: "1.0.0",
		Os:      "linux",
		Arch:    "amd64",
	}

	w := doRequest(srv, http.MethodPost, "/api/v1/updates/push", userToken, body)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestPushUpdate_VersionMismatch(t *testing.T) {
	srv, adminToken, _ := newTestServerWithUpdater(t)

	// Publish v1.0.0
	publish := PublishUpdateRequest{
		Version: "1.0.0",
		Os:      "linux",
		Arch:    "amd64",
		Url:     "https://example.com/agent",
		Sha256:  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
	}
	w := doRequest(srv, http.MethodPost, "/api/v1/updates/manifests", adminToken, publish)
	require.Equal(t, http.StatusOK, w.Code)

	// Push v2.0.0 (not published)
	push := PushUpdateRequest{
		Version: "2.0.0",
		Os:      "linux",
		Arch:    "amd64",
	}
	w = doRequest(srv, http.MethodPost, "/api/v1/updates/push", adminToken, push)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPushUpdate_NoConnectedAgents(t *testing.T) {
	srv, adminToken, _ := newTestServerWithUpdater(t)

	// Publish
	publish := PublishUpdateRequest{
		Version: "1.0.0",
		Os:      "linux",
		Arch:    "amd64",
		Url:     "https://example.com/agent",
		Sha256:  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
	}
	w := doRequest(srv, http.MethodPost, "/api/v1/updates/manifests", adminToken, publish)
	require.Equal(t, http.StatusOK, w.Code)

	// Push (no agents connected)
	push := PushUpdateRequest{
		Version: "1.0.0",
		Os:      "linux",
		Arch:    "amd64",
	}
	w = doRequest(srv, http.MethodPost, "/api/v1/updates/push", adminToken, push)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp PushUpdateResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 0, resp.PushedCount)
}

func TestGetUpdateSigningKey_Admin(t *testing.T) {
	srv, adminToken, _ := newTestServerWithUpdater(t)

	w := doRequest(srv, http.MethodGet, "/api/v1/updates/signing-key", adminToken, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		PublicKey string `json:"public_key"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Len(t, resp.PublicKey, 64) // 32 bytes = 64 hex chars
}

func TestGetUpdateSigningKey_NonAdmin(t *testing.T) {
	srv, _, userToken := newTestServerWithUpdater(t)

	w := doRequest(srv, http.MethodGet, "/api/v1/updates/signing-key", userToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
