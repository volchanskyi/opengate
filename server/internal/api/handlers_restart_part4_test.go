package api

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

func TestGetDeviceLogs(t *testing.T) {
	t.Parallel()
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
				entries := []device.LogEntry{
					{Timestamp: "2026-04-01T12:00:00Z", Level: "INFO", Target: "mesh_agent::main", Message: "agent started"},
					{Timestamp: "2026-04-01T12:01:00Z", Level: "WARN", Target: "mesh_agent::connection", Message: "slow heartbeat"},
				}
				require.NoError(t, env.deviceLogs.Upsert(env.ctx, env.device.ID, entries))
			}

			token := env.ownerToken
			if !tt.owned {
				otherUser := testutil.SeedUser(t, env.ctx, env.store)
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
		entries := []device.LogEntry{
			{Timestamp: "2026-04-01T12:00:00Z", Level: "INFO", Target: "test", Message: "cached"},
		}
		require.NoError(t, env.deviceLogs.Upsert(env.ctx, env.device.ID, entries))

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

	t.Run("old agent without logs capability is not sent request", func(t *testing.T) {
		env := setupDeviceTest(t, true)
		ac := env.srv.agents.GetAgent(env.device.ID)
		require.NotNil(t, ac)
		ac.Capabilities = nil

		w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.device.ID.String()+"/logs", env.ownerToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Zero(t, env.agentStream.Len())
	})
}
