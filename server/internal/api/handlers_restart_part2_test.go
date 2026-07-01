package api

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
	"time"
)

func TestRestartDevice(t *testing.T) {
	t.Parallel()
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
				otherUser := testutil.SeedUser(t, env.ctx, env.store)
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

		// auditLog is async (fire-and-forget goroutine) — poll until it lands.
		var events []*audit.Event
		require.Eventually(t, func() bool {
			var err error
			events, err = env.srv.audit.Query(env.ctx, audit.Query{Action: "device.restart"})
			return err == nil && len(events) == 1
		}, 2*time.Second, 25*time.Millisecond, "device.restart audit event should be written")
		assert.Equal(t, env.device.ID.String(), events[0].Target)
	})
}
