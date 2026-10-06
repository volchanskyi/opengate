package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

func newTestServerWithUpdater(t *testing.T) (*Server, string, string) {
	t.Helper()
	return newTestServerWithUpdaterAndAgents(t, &stubAgentGetter{})
}

func newTestServerWithUpdaterAndAgents(t *testing.T, agents AgentGetter) (*Server, string, string) {
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
		Sites:          testutil.NewTestSites(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         agents,
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

func samplePublish(v string) PublishUpdateRequest {
	return PublishUpdateRequest{
		Version: v,
		Os:      "linux",
		Arch:    "amd64",
		Url:     "https://example.com/agent",
		Sha256:  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
	}
}

func samplePush(v string) PushUpdateRequest {
	return PushUpdateRequest{Version: v, Os: "linux", Arch: "amd64"}
}

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

func assertPushNotFound(t *testing.T, published, pushed string) {
	t.Helper()
	srv, adminToken, _ := newTestServerWithUpdater(t)
	if published != "" {
		publishManifest(t, srv, adminToken, published)
	}
	w := doRequest(srv, http.MethodPost, "/api/v1/updates/push", adminToken, samplePush(pushed))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPushUpdate_NoManifest(t *testing.T) {
	t.Parallel()
	assertPushNotFound(t, "", "1.0.0")
}

func TestPushUpdate_VersionMismatch(t *testing.T) {
	t.Parallel()
	assertPushNotFound(t, "1.0.0", "2.0.0")
}

func TestPushUpdate_NoConnectedAgents(t *testing.T) {
	t.Parallel()
	srv, adminToken, _ := newTestServerWithUpdater(t)

	publishManifest(t, srv, adminToken, "1.0.0")

	w := doRequest(srv, http.MethodPost, "/api/v1/updates/push", adminToken, samplePush("1.0.0"))
	assert.Equal(t, http.StatusOK, w.Code)

	var resp PushUpdateResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 0, resp.PushedCount)
}

func pushToFailingAgent(t *testing.T, updateErr error) (*httptest.ResponseRecorder, *fakeAgentControl) {
	t.Helper()
	fake := &fakeAgentControl{
		meta:      agentapi.AgentMeta{DeviceID: uuid.New(), OS: "linux", Arch: "amd64", AgentVersion: "0.9.0"},
		updateErr: updateErr,
	}
	agents := &stubAgentGetter{agents: map[protocol.DeviceID]AgentControl{fake.meta.DeviceID: fake}}
	srv, adminToken, _ := newTestServerWithUpdaterAndAgents(t, agents)
	publishManifest(t, srv, adminToken, "1.0.0")
	return doRequest(srv, http.MethodPost, "/api/v1/updates/push", adminToken, samplePush("1.0.0")), fake
}

func TestPushUpdate_UndeliverableManifest(t *testing.T) {
	t.Parallel()

	w, fake := pushToFailingAgent(t, fmt.Errorf("%w: %s.signature is empty", agentapi.ErrIncompleteControlMessage, protocol.MsgAgentUpdate))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 1, fake.updateCalls)
}

func TestPushUpdate_PerAgentSendFailureIsSkipped(t *testing.T) {
	t.Parallel()

	w, fake := pushToFailingAgent(t, errors.New("stream closed"))

	require.Equal(t, http.StatusOK, w.Code)
	var resp PushUpdateResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 0, resp.PushedCount)
	assert.Equal(t, 1, fake.updateCalls)
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
