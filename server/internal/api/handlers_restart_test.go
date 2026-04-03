package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// deviceTestEnv holds common setup for restart and hardware handler tests.
type deviceTestEnv struct {
	store       db.Store
	device      *db.Device
	srv         *Server
	ownerToken  string
	agentStream *bytes.Buffer
	generateToken func(userID uuid.UUID, email string, isAdmin bool) (string, error)
}

// setupDeviceTest creates a user, group, device, and test server. When online
// is true an AgentConn backed by agentStream is registered.
func setupDeviceTest(t *testing.T, online bool) *deviceTestEnv {
	t.Helper()

	var agentStream bytes.Buffer
	store := testutil.NewTestStore(t)
	ctx := t.Context()

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	device := testutil.SeedDevice(t, ctx, store, group.ID)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	lookup := &stubAgentGetter{}
	if online {
		ac := agentapi.NewAgentConn(device.ID, group.ID, &agentStream, store, logger)
		lookup = &stubAgentGetter{
			agents: map[protocol.DeviceID]*agentapi.AgentConn{device.ID: ac},
		}
	}

	srv, cfg := newTestServerWithAgents(t, lookup, relay.NewRelay(slog.Default()))
	srv.store = store

	token, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	return &deviceTestEnv{
		store:         store,
		device:        device,
		srv:           srv,
		ownerToken:    token,
		agentStream:   &agentStream,
		generateToken: cfg.GenerateToken,
	}
}

func TestRestartDevice(t *testing.T) {
	tests := []struct {
		name       string
		online     bool
		found      bool
		owned      bool
		wantStatus int
	}{
		{"online agent", true, true, true, http.StatusOK},
		{"agent not connected", false, true, true, http.StatusConflict},
		{"device not found", false, false, true, http.StatusNotFound},
		{"not owner", true, true, false, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupDeviceTest(t, tt.online)

			token := env.ownerToken
			if !tt.owned {
				otherUser := testutil.SeedUser(t, t.Context(), env.store)
				var err error
				token, err = env.generateToken(otherUser.ID, otherUser.Email, otherUser.IsAdmin)
				require.NoError(t, err)
			}

			targetID := env.device.ID
			if !tt.found {
				targetID = uuid.New()
			}

			w := doRequest(env.srv, http.MethodPost, "/api/v1/devices/"+targetID.String()+"/restart", token, map[string]string{
				"reason": "test restart",
			})

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantStatus == http.StatusOK && tt.online {
				// Verify the RestartAgent message was written to the agent stream
				codec := &protocol.Codec{}
				frameType, payload, err := codec.ReadFrame(env.agentStream)
				require.NoError(t, err)
				assert.Equal(t, byte(protocol.FrameControl), frameType)

				msg, err := codec.DecodeControl(payload)
				require.NoError(t, err)
				assert.Equal(t, protocol.MsgRestartAgent, msg.Type)
				assert.Equal(t, "test restart", msg.Reason)
			}
		})
	}

	t.Run("requires auth", func(t *testing.T) {
		srv, _ := newTestServer(t)
		w := doRequest(srv, http.MethodPost, "/api/v1/devices/"+uuid.New().String()+"/restart", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("default reason when body is nil", func(t *testing.T) {
		env := setupDeviceTest(t, true)

		w := doRawRequest(env.srv, http.MethodPost, "/api/v1/devices/"+env.device.ID.String()+"/restart", env.ownerToken, "")
		assert.Equal(t, http.StatusOK, w.Code)

		codec := &protocol.Codec{}
		_, payload, err := codec.ReadFrame(env.agentStream)
		require.NoError(t, err)
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err)
		assert.Equal(t, "restart requested from web UI", msg.Reason)
	})

	t.Run("audit log written", func(t *testing.T) {
		env := setupDeviceTest(t, true)

		w := doRequest(env.srv, http.MethodPost, "/api/v1/devices/"+env.device.ID.String()+"/restart", env.ownerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// auditLog is async (fire-and-forget goroutine)
		time.Sleep(100 * time.Millisecond)

		events, err := env.store.QueryAuditLog(t.Context(), db.AuditQuery{Action: "device.restart"})
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, env.device.ID.String(), events[0].Target)
	})
}

func TestGetDeviceHardware(t *testing.T) {
	tests := []struct {
		name       string
		hasCached  bool
		online     bool
		found      bool
		owned      bool
		wantStatus int
	}{
		{"cached data available", true, true, true, true, http.StatusOK},
		{"no cache but online triggers request", false, true, true, true, http.StatusAccepted},
		{"no cache and offline", false, false, true, true, http.StatusNotFound},
		{"device not found", false, false, false, true, http.StatusNotFound},
		{"not owner", false, true, true, false, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupDeviceTest(t, tt.online)

			if tt.hasCached {
				hw := &db.DeviceHardware{
					DeviceID:    env.device.ID,
					CPUModel:    "Intel Core i7-12700K",
					CPUCores:    12,
					RAMTotalMB:  32768,
					DiskTotalMB: 512000,
					DiskFreeMB:  256000,
					NetworkInterfaces: []db.NetworkInterfaceInfo{
						{Name: "eth0", MAC: "00:11:22:33:44:55", IPv4: []string{"192.168.1.100"}, IPv6: []string{}},
					},
				}
				require.NoError(t, env.store.UpsertDeviceHardware(t.Context(), hw))
			}

			token := env.ownerToken
			if !tt.owned {
				otherUser := testutil.SeedUser(t, t.Context(), env.store)
				var err error
				token, err = env.generateToken(otherUser.ID, otherUser.Email, otherUser.IsAdmin)
				require.NoError(t, err)
			}

			targetID := env.device.ID
			if !tt.found {
				targetID = uuid.New()
			}

			w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+targetID.String()+"/hardware", token, nil)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantStatus == http.StatusOK {
				var hw DeviceHardware
				require.NoError(t, json.NewDecoder(w.Body).Decode(&hw))
				assert.Equal(t, "Intel Core i7-12700K", hw.CpuModel)
				assert.Equal(t, 12, hw.CpuCores)
				assert.Equal(t, int64(32768), hw.RamTotalMb)
			}

			if tt.wantStatus == http.StatusAccepted && tt.online {
				// Verify RequestHardwareReport was sent to agent
				codec := &protocol.Codec{}
				_, payload, err := codec.ReadFrame(env.agentStream)
				require.NoError(t, err)
				msg, err := codec.DecodeControl(payload)
				require.NoError(t, err)
				assert.Equal(t, protocol.MsgRequestHardwareReport, msg.Type)
			}
		})
	}

	t.Run("requires auth", func(t *testing.T) {
		srv, _ := newTestServer(t)
		w := doRequest(srv, http.MethodGet, "/api/v1/devices/"+uuid.New().String()+"/hardware", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestGetDeviceLogs(t *testing.T) {
	tests := []struct {
		name       string
		hasCached  bool
		online     bool
		found      bool
		owned      bool
		wantStatus int
	}{
		{"cached data available", true, true, true, true, http.StatusOK},
		{"stale cache served when offline", true, false, true, true, http.StatusOK},
		{"no cache agent online triggers request", false, true, true, true, http.StatusAccepted},
		{"device not found", false, false, false, true, http.StatusNotFound},
		{"wrong group ownership", false, true, true, false, http.StatusForbidden},
		{"no cache agent offline", false, false, true, true, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupDeviceTest(t, tt.online)

			if tt.hasCached {
				entries := []db.DeviceLogEntry{
					{Timestamp: "2026-04-01T12:00:00Z", Level: "INFO", Target: "mesh_agent::main", Message: "agent started"},
					{Timestamp: "2026-04-01T12:01:00Z", Level: "WARN", Target: "mesh_agent::connection", Message: "slow heartbeat"},
				}
				require.NoError(t, env.store.UpsertDeviceLogs(t.Context(), env.device.ID, entries))
			}

			token := env.ownerToken
			if !tt.owned {
				otherUser := testutil.SeedUser(t, t.Context(), env.store)
				var err error
				token, err = env.generateToken(otherUser.ID, otherUser.Email, otherUser.IsAdmin)
				require.NoError(t, err)
			}

			targetID := env.device.ID
			if !tt.found {
				targetID = uuid.New()
			}

			w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+targetID.String()+"/logs", token, nil)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantStatus == http.StatusOK {
				var resp DeviceLogsResponse
				require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
				assert.Equal(t, 2, resp.Total)
				assert.Len(t, resp.Entries, 2)
			}

			if tt.wantStatus == http.StatusAccepted && tt.online {
				// Verify RequestDeviceLogs was sent to agent
				codec := &protocol.Codec{}
				_, payload, err := codec.ReadFrame(env.agentStream)
				require.NoError(t, err)
				msg, err := codec.DecodeControl(payload)
				require.NoError(t, err)
				assert.Equal(t, protocol.MsgRequestDeviceLogs, msg.Type)
			}
		})
	}

	t.Run("refresh bypasses cache", func(t *testing.T) {
		env := setupDeviceTest(t, true)

		// Seed cached logs so hasRecent returns true.
		entries := []db.DeviceLogEntry{
			{Timestamp: "2026-04-01T12:00:00Z", Level: "INFO", Target: "test", Message: "cached"},
		}
		require.NoError(t, env.store.UpsertDeviceLogs(t.Context(), env.device.ID, entries))

		// Without refresh, should return cached data (200).
		w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.device.ID.String()+"/logs", env.ownerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// With refresh=true, should bypass cache and request from agent (202).
		w = doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.device.ID.String()+"/logs?refresh=true", env.ownerToken, nil)
		assert.Equal(t, http.StatusAccepted, w.Code)
	})

	t.Run("requires auth", func(t *testing.T) {
		srv, _ := newTestServer(t)
		w := doRequest(srv, http.MethodGet, "/api/v1/devices/"+uuid.New().String()+"/logs", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
