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
		Relay:     relay.NewRelay(slog.Default()),
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

func TestGetUpdateStatus_Empty(t *testing.T) {
	srv, adminToken, _ := newTestServerWithUpdater(t)

	w := doRequest(srv, http.MethodGet, "/api/v1/updates/status/1.0.0", adminToken, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var updates []DeviceUpdate
	require.NoError(t, json.NewDecoder(w.Body).Decode(&updates))
	assert.Empty(t, updates)
}

func TestGetUpdateStatus_NonAdmin(t *testing.T) {
	srv, _, userToken := newTestServerWithUpdater(t)

	w := doRequest(srv, http.MethodGet, "/api/v1/updates/status/1.0.0", userToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestNormalizeOS(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"already linux", "linux", "linux"},
		{"already windows", "windows", "windows"},
		{"ubuntu pretty name", "Ubuntu 22.04.4 LTS", "linux"},
		{"debian pretty name", "Debian GNU/Linux 12 (bookworm)", "linux"},
		{"fedora", "Fedora Linux 40 (Server Edition)", "linux"},
		{"centos", "CentOS Stream 9", "linux"},
		{"alpine", "Alpine Linux v3.19", "linux"},
		{"arch linux", "Arch Linux", "linux"},
		{"rhel", "Red Hat Enterprise Linux 9.3 (Plow)", "linux"},
		{"windows pretty", "Windows 11 Pro", "windows"},
		{"darwin", "darwin", "darwin"},
		{"macos", "macOS 14.4", "darwin"},
		{"unknown", "freebsd", "freebsd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeOS(tt.input))
		})
	}
}

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"x86_64 to amd64", "x86_64", "amd64"},
		{"aarch64 to arm64", "aarch64", "arm64"},
		{"already amd64", "amd64", "amd64"},
		{"already arm64", "arm64", "arm64"},
		{"unknown passthrough", "riscv64", "riscv64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeArch(tt.input))
		})
	}
}
