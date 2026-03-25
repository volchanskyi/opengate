package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// publishManifest publishes an update manifest via the REST API.
func publishManifest(t *testing.T, env *sessionTestEnv, jwt, version, osName, arch string) api.AgentManifest {
	t.Helper()

	hash := sha256.Sum256([]byte("fake-binary-" + version))
	body := api.PublishUpdateRequest{
		Version: version,
		Os:      osName,
		Arch:    arch,
		Url:     "https://example.com/agent-" + version,
		Sha256:  hex.EncodeToString(hash[:]),
	}

	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))

	req, err := http.NewRequest(http.MethodPost, env.httpSrv.URL+"/api/v1/updates/manifests", &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", bearerPrefix+jwt)

	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var manifest api.AgentManifest
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&manifest))
	assert.Equal(t, version, manifest.Version)
	assert.NotEmpty(t, manifest.Signature, "manifest should be signed")
	return manifest
}

// pushUpdate pushes an update to connected agents via the REST API.
func pushUpdate(t *testing.T, env *sessionTestEnv, jwt, version, osName, arch string) api.PushUpdateResponse {
	t.Helper()

	body := api.PushUpdateRequest{
		Version: version,
		Os:      osName,
		Arch:    arch,
	}

	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))

	req, err := http.NewRequest(http.MethodPost, env.httpSrv.URL+"/api/v1/updates/push", &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", bearerPrefix+jwt)

	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result api.PushUpdateResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	return result
}

// TestUpdatePublishAndPush verifies the full update flow:
// admin publishes manifest → pushes update → connected agent receives AgentUpdate
// control message on QUIC stream → agent sends AgentUpdateAck → DB records status.
func TestUpdatePublishAndPush(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	admin, _ := testutil.SeedAdminUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, admin.ID)

	adminJWT, err := env.jwt.GenerateToken(admin.ID, admin.Email, admin.IsAdmin)
	require.NoError(t, err)

	// Connect an agent that reports as linux/amd64 version 0.13.0
	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// 1. Publish manifest for linux/amd64 v0.14.0
	manifest := publishManifest(t, env, adminJWT, "0.14.0", "linux", "amd64")
	assert.Equal(t, "0.14.0", manifest.Version)

	// 2. Push update to all eligible agents
	result := pushUpdate(t, env, adminJWT, "0.14.0", "linux", "amd64")
	assert.Equal(t, 1, result.PushedCount, "one agent should receive the update")

	// 3. Agent should receive AgentUpdate on QUIC control stream
	codec := &protocol.Codec{}
	frameType, payload, err := codec.ReadFrame(stream)
	require.NoError(t, err)
	assert.Equal(t, protocol.FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgAgentUpdate, msg.Type)
	assert.Equal(t, "0.14.0", msg.Version)
	assert.Contains(t, msg.URL, "agent-0.14.0")
	assert.NotEmpty(t, msg.SHA256)
	assert.NotEmpty(t, msg.Signature)

	// 4. Agent sends AgentUpdateAck back
	success := true
	ackMsg := &protocol.ControlMessage{
		Type:    protocol.MsgAgentUpdateAck,
		Version: "0.14.0",
		Success: &success,
	}
	ackPayload, err := codec.EncodeControl(ackMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, ackPayload))

	// 5. Verify DB recorded the pending update (created synchronously in the push handler)
	updates, err := env.store.ListDeviceUpdatesByVersion(ctx, "0.14.0")
	require.NoError(t, err)
	require.NotEmpty(t, updates, "push handler should have created a device_update record")

	found := false
	for _, u := range updates {
		if u.DeviceID == deviceID {
			// Status may be "pending" or "success" depending on whether the
			// server processed the AgentUpdateAck before this query runs.
			assert.Contains(t, []db.UpdateStatus{db.UpdateStatusPending, db.UpdateStatusSuccess}, u.Status)
			found = true
		}
	}
	assert.True(t, found, "device update record should exist for device %s", deviceID)
}

// TestUpdatePushSkipsCurrentVersion verifies that agents already on the
// target version are not pushed an update.
func TestUpdatePushSkipsCurrentVersion(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	admin, _ := testutil.SeedAdminUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, admin.ID)

	adminJWT, err := env.jwt.GenerateToken(admin.ID, admin.Email, admin.IsAdmin)
	require.NoError(t, err)

	// Connect agent — it will register with the version from AGENT_VERSION env
	// (defaults to Cargo.toml version). We publish a manifest matching that version.
	_, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Get the agent's reported version from the DB
	device, err := env.store.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	agentVersion := device.AgentVersion

	// Publish manifest with the same version the agent already reports
	publishManifest(t, env, adminJWT, agentVersion, "linux", "amd64")

	// Push should skip — agent is already on this version
	result := pushUpdate(t, env, adminJWT, agentVersion, "linux", "amd64")
	assert.Equal(t, 0, result.PushedCount, "agent already on target version should be skipped")
}

// TestUpdatePushNoMatchingOS verifies that agents with non-matching OS/arch
// are not pushed an update.
func TestUpdatePushNoMatchingOS(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	admin, _ := testutil.SeedAdminUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, admin.ID)

	adminJWT, err := env.jwt.GenerateToken(admin.ID, admin.Email, admin.IsAdmin)
	require.NoError(t, err)

	// Connect agent — registers as linux/amd64
	_, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Publish manifest for windows/amd64 — won't match the linux agent
	publishManifest(t, env, adminJWT, "0.15.0", "windows", "amd64")

	// Push for windows/amd64 — linux agent should not be targeted
	result := pushUpdate(t, env, adminJWT, "0.15.0", "windows", "amd64")
	assert.Equal(t, 0, result.PushedCount, "linux agent should not get windows update")
}
