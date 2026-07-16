package api

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

func TestGetDeviceHardware(t *testing.T) {
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
		{"no cache but online triggers request", false, true, true, true, http.StatusAccepted},
		{"no cache and offline", false, false, true, true, http.StatusNotFound},
		{"device not found", false, false, false, true, http.StatusNotFound},
		{"not owner", false, true, true, false, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupDeviceTest(t, tt.online)

			if tt.hasCached {
				hw := &device.Hardware{
					DeviceID:    env.device.ID,
					CPUModel:    "Intel Core i7-12700K",
					CPUCores:    12,
					RAMTotalMB:  32768,
					DiskTotalMB: 512000,
					DiskFreeMB:  256000,
					NetworkInterfaces: []device.NetworkInterfaceInfo{
						{Name: "eth0", MAC: "00:11:22:33:44:55", IPv4: []string{"192.168.1.100"}, IPv6: []string{}},
					},
				}
				require.NoError(t, env.hardware.Upsert(env.ctx, hw))
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

	t.Run("old agent without hardware capability is not sent request", func(t *testing.T) {
		env := setupDeviceTest(t, true)
		ac := env.srv.agents.GetAgent(env.device.ID)
		require.NotNil(t, ac)
		// The stored value is the concrete conn; reach its field to simulate an
		// agent that never advertised the capability.
		ac.(*agentapi.AgentConn).Capabilities = nil

		w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.device.ID.String()+"/hardware", env.ownerToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Zero(t, env.agentStream.Len())
	})
}
