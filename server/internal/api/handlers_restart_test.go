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
			var agentStream bytes.Buffer
			store := testutil.NewTestStore(t)
			ctx := t.Context()

			user := testutil.SeedUser(t, ctx, store)
			group := testutil.SeedGroup(t, ctx, store, user.ID)
			device := testutil.SeedDevice(t, ctx, store, group.ID)

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

			lookup := &stubAgentGetter{}
			if tt.online {
				ac := agentapi.NewAgentConn(device.ID, group.ID, &agentStream, store, logger)
				lookup = &stubAgentGetter{
					agents: map[protocol.DeviceID]*agentapi.AgentConn{
						device.ID: ac,
					},
				}
			}

			srv, cfg := newTestServerWithAgents(t, lookup, relay.NewRelay(slog.Default()))
			srv.store = store

			var token string
			if tt.owned {
				var err error
				token, err = cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
				require.NoError(t, err)
			} else {
				otherUser := testutil.SeedUser(t, ctx, store)
				var err error
				token, err = cfg.GenerateToken(otherUser.ID, otherUser.Email, otherUser.IsAdmin)
				require.NoError(t, err)
			}

			targetID := device.ID
			if !tt.found {
				targetID = uuid.New()
			}

			w := doRequest(srv, http.MethodPost, "/api/v1/devices/"+targetID.String()+"/restart", token, map[string]string{
				"reason": "test restart",
			})

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantStatus == http.StatusOK && tt.online {
				// Verify the RestartAgent message was written to the agent stream
				codec := &protocol.Codec{}
				frameType, payload, err := codec.ReadFrame(&agentStream)
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
		var agentStream bytes.Buffer
		store := testutil.NewTestStore(t)
		ctx := t.Context()

		user := testutil.SeedUser(t, ctx, store)
		group := testutil.SeedGroup(t, ctx, store, user.ID)
		device := testutil.SeedDevice(t, ctx, store, group.ID)

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		ac := agentapi.NewAgentConn(device.ID, group.ID, &agentStream, store, logger)

		lookup := &stubAgentGetter{
			agents: map[protocol.DeviceID]*agentapi.AgentConn{device.ID: ac},
		}

		srv, cfg := newTestServerWithAgents(t, lookup, relay.NewRelay(slog.Default()))
		srv.store = store

		token, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
		require.NoError(t, err)

		// Send request with no body
		w := doRawRequest(srv, http.MethodPost, "/api/v1/devices/"+device.ID.String()+"/restart", token, "")
		assert.Equal(t, http.StatusOK, w.Code)

		codec := &protocol.Codec{}
		_, payload, err := codec.ReadFrame(&agentStream)
		require.NoError(t, err)
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err)
		assert.Equal(t, "restart requested from web UI", msg.Reason)
	})

	t.Run("audit log written", func(t *testing.T) {
		var agentStream bytes.Buffer
		store := testutil.NewTestStore(t)
		ctx := t.Context()

		user := testutil.SeedUser(t, ctx, store)
		group := testutil.SeedGroup(t, ctx, store, user.ID)
		device := testutil.SeedDevice(t, ctx, store, group.ID)

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		ac := agentapi.NewAgentConn(device.ID, group.ID, &agentStream, store, logger)

		lookup := &stubAgentGetter{
			agents: map[protocol.DeviceID]*agentapi.AgentConn{device.ID: ac},
		}

		srv, cfg := newTestServerWithAgents(t, lookup, relay.NewRelay(slog.Default()))
		srv.store = store

		token, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
		require.NoError(t, err)

		w := doRequest(srv, http.MethodPost, "/api/v1/devices/"+device.ID.String()+"/restart", token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// auditLog is async (fire-and-forget goroutine)
		time.Sleep(100 * time.Millisecond)

		events, err := store.QueryAuditLog(ctx, db.AuditQuery{Action: "device.restart"})
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, user.ID, events[0].UserID)
		assert.Equal(t, device.ID.String(), events[0].Target)
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
			var agentStream bytes.Buffer
			store := testutil.NewTestStore(t)
			ctx := t.Context()

			user := testutil.SeedUser(t, ctx, store)
			group := testutil.SeedGroup(t, ctx, store, user.ID)
			device := testutil.SeedDevice(t, ctx, store, group.ID)

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

			lookup := &stubAgentGetter{}
			if tt.online {
				ac := agentapi.NewAgentConn(device.ID, group.ID, &agentStream, store, logger)
				lookup = &stubAgentGetter{
					agents: map[protocol.DeviceID]*agentapi.AgentConn{device.ID: ac},
				}
			}

			srv, cfg := newTestServerWithAgents(t, lookup, relay.NewRelay(slog.Default()))
			srv.store = store

			if tt.hasCached {
				hw := &db.DeviceHardware{
					DeviceID:    device.ID,
					CPUModel:    "Intel Core i7-12700K",
					CPUCores:    12,
					RAMTotalMB:  32768,
					DiskTotalMB: 512000,
					DiskFreeMB:  256000,
					NetworkInterfaces: []db.NetworkInterfaceInfo{
						{Name: "eth0", MAC: "00:11:22:33:44:55", IPv4: []string{"192.168.1.100"}, IPv6: []string{}},
					},
				}
				require.NoError(t, store.UpsertDeviceHardware(ctx, hw))
			}

			var token string
			if tt.owned {
				var err error
				token, err = cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
				require.NoError(t, err)
			} else {
				otherUser := testutil.SeedUser(t, ctx, store)
				var err error
				token, err = cfg.GenerateToken(otherUser.ID, otherUser.Email, otherUser.IsAdmin)
				require.NoError(t, err)
			}

			targetID := device.ID
			if !tt.found {
				targetID = uuid.New()
			}

			w := doRequest(srv, http.MethodGet, "/api/v1/devices/"+targetID.String()+"/hardware", token, nil)

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
				_, payload, err := codec.ReadFrame(&agentStream)
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
