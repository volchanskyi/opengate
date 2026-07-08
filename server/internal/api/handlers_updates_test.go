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
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		DeviceUpdates:  testutil.NewTestDeviceUpdates(t, store),
		Enrollment:     testutil.NewTestEnrollment(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Groups:         testutil.NewTestGroups(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		AMTDevices:     testutil.NewTestAMTDevices(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         &stubAgentGetter{},
		AMT:            &stubAMTOperator{},
		Relay:          relay.NewRelay(slog.Default()),
		Notifier:       &notifications.NoopNotifier{},
		Signing:        signing,
		Manifests:      manifests,
		Logger:         logger,
	})

	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)
	_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

	return srv, adminToken, userToken
}

// TestUpdateEndpoints_EmptyList pins that the manifest and status list
// endpoints return an empty JSON array before anything is published.
func TestUpdateEndpoints_EmptyList(t *testing.T) {
	t.Parallel()
	srv, adminToken, _ := newTestServerWithUpdater(t)

	for _, path := range []string{"/api/v1/updates/manifests", "/api/v1/updates/status/1.0.0"} {
		w := doRequest(srv, http.MethodGet, path, adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var items []json.RawMessage
		require.NoError(t, json.NewDecoder(w.Body).Decode(&items))
		assert.Empty(t, items, path)
	}
}

// samplePublish returns a valid publish request for version v.
func samplePublish(v string) PublishUpdateRequest {
	return PublishUpdateRequest{
		Version: v,
		Os:      "linux",
		Arch:    "amd64",
		Url:     "https://example.com/agent",
		Sha256:  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
	}
}

// samplePush returns a push request for version v.
func samplePush(v string) PushUpdateRequest {
	return PushUpdateRequest{Version: v, Os: "linux", Arch: "amd64"}
}

// publishManifest publishes version v and asserts the request succeeded.
func publishManifest(t *testing.T, srv *Server, token, v string) {
	t.Helper()
	w := doRequest(srv, http.MethodPost, "/api/v1/updates/manifests", token, samplePublish(v))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestPublishUpdate_Success(t *testing.T) {
	t.Parallel()
	srv, adminToken, _ := newTestServerWithUpdater(t)

	body := samplePublish("1.0.0")

	w := doRequest(srv, http.MethodPost, "/api/v1/updates/manifests", adminToken, body)
	assert.Equal(t, http.StatusOK, w.Code)

	var manifest AgentManifest
	require.NoError(t, json.NewDecoder(w.Body).Decode(&manifest))
	assert.Equal(t, "1.0.0", manifest.Version)
	assert.Equal(t, "linux", manifest.Os)
	assert.Equal(t, "amd64", manifest.Arch)
	assert.NotEmpty(t, manifest.Signature)
}

func TestListUpdateManifests_AfterPublish(t *testing.T) {
	t.Parallel()
	srv, adminToken, _ := newTestServerWithUpdater(t)

	publishManifest(t, srv, adminToken, "1.0.0")

	w := doRequest(srv, http.MethodGet, "/api/v1/updates/manifests", adminToken, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var manifests []AgentManifest
	require.NoError(t, json.NewDecoder(w.Body).Decode(&manifests))
	assert.Len(t, manifests, 1)
	assert.Equal(t, "1.0.0", manifests[0].Version)
}

func TestPushUpdate_NoManifest(t *testing.T) {
	t.Parallel()
	srv, adminToken, _ := newTestServerWithUpdater(t)

	body := samplePush("1.0.0")

	w := doRequest(srv, http.MethodPost, "/api/v1/updates/push", adminToken, body)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPushUpdate_VersionMismatch(t *testing.T) {
	t.Parallel()
	srv, adminToken, _ := newTestServerWithUpdater(t)

	publishManifest(t, srv, adminToken, "1.0.0")

	// Push v2.0.0 (not published)
	w := doRequest(srv, http.MethodPost, "/api/v1/updates/push", adminToken, samplePush("2.0.0"))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPushUpdate_NoConnectedAgents(t *testing.T) {
	t.Parallel()
	srv, adminToken, _ := newTestServerWithUpdater(t)

	publishManifest(t, srv, adminToken, "1.0.0")

	// Push (no agents connected)
	w := doRequest(srv, http.MethodPost, "/api/v1/updates/push", adminToken, samplePush("1.0.0"))
	assert.Equal(t, http.StatusOK, w.Code)

	var resp PushUpdateResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 0, resp.PushedCount)
}

func TestGetUpdateSigningKey_Admin(t *testing.T) {
	t.Parallel()
	srv, adminToken, _ := newTestServerWithUpdater(t)

	w := doRequest(srv, http.MethodGet, "/api/v1/updates/signing-key", adminToken, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		PublicKey string `json:"public_key"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Len(t, resp.PublicKey, 64) // 32 bytes = 64 hex chars
}

// TestUpdateEndpoints_NonAdmin pins that every update endpoint rejects a
// non-admin caller with 403.
func TestUpdateEndpoints_NonAdmin(t *testing.T) {
	t.Parallel()
	srv, _, userToken := newTestServerWithUpdater(t)

	tests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"publish", http.MethodPost, "/api/v1/updates/manifests", samplePublish("1.0.0")},
		{"push", http.MethodPost, "/api/v1/updates/push", samplePush("1.0.0")},
		{"signing-key", http.MethodGet, "/api/v1/updates/signing-key", nil},
		{"status", http.MethodGet, "/api/v1/updates/status/1.0.0", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequest(srv, tt.method, tt.path, userToken, tt.body)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}

// NormalizeOS and NormalizeArch tests are in the osutil package.
