package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// TestDeviceLogsBroker_RoundTripRedactedAndAudited exercises the transient raw
// broker end to end: an admin pull blocks until the online agent responds, the
// bounded lines are streamed straight back (nothing persisted), server-side
// redaction strips a secret even though the agent sent it in the clear, and an
// audit event is recorded for the pull.
func TestDeviceLogsBroker_RoundTripRedactedAndAudited(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)
	adminToken, err := env.jwt.GenerateToken(user.ID, user.Email, true)
	require.NoError(t, err)

	stream, deviceID := env.connectAgent(t, group.ID)
	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Fake agent: answer the server's RequestDeviceLogs with a secret-bearing line.
	respErr := make(chan error, 1)
	go func() {
		codec := &protocol.Codec{}
		for {
			ft, payload, rerr := codec.ReadFrame(stream)
			if rerr != nil {
				respErr <- rerr
				return
			}
			if ft != protocol.FrameControl {
				continue
			}
			msg, derr := codec.DecodeControl(payload)
			if derr != nil {
				respErr <- derr
				return
			}
			if msg.Type != protocol.MsgRequestDeviceLogs {
				continue
			}
			out, eerr := codec.EncodeControl(&protocol.ControlMessage{
				Type: protocol.MsgDeviceLogsResponse,
				LogEntries: []protocol.LogEntry{
					{Timestamp: "2026-04-01T12:00:00Z", Level: "INFO", Target: "app", Message: "login ok"},
					{Timestamp: "2026-04-01T12:00:01Z", Level: "WARN", Target: "auth", Message: "Authorization: Bearer abcdef0123456789 rejected"},
				},
				TotalCount: 2,
			})
			if eerr != nil {
				respErr <- eerr
				return
			}
			respErr <- codec.WriteFrame(stream, protocol.FrameControl, out)
			return
		}
	}()

	req, err := http.NewRequest(http.MethodGet, env.httpSrv.URL+"/api/v1/devices/"+deviceID.String()+"/logs", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", bearerPrefix+adminToken)
	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, <-respErr)

	var body struct {
		Entries []struct {
			Message string `json:"message"`
		} `json:"entries"`
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body.Entries, 2)
	assert.Equal(t, 2, body.Total)
	assert.NotContains(t, body.Entries[1].Message, "abcdef0123456789", "server must redact the secret even when the agent sends it in clear")
	assert.Contains(t, body.Entries[1].Message, "[REDACTED]")

	// The pull is audited (fire-and-forget write).
	auditRepo := testutil.NewTestAudit(t, env.store)
	require.Eventually(t, func() bool {
		evs, qerr := auditRepo.Query(defaultTenantContext(), audit.Query{Action: "device.logs.read", Limit: 10})
		return qerr == nil && len(evs) >= 1
	}, 3*time.Second, 50*time.Millisecond)
}

// TestDeviceLogsBroker_NonAdminForbidden pins the elevated-permission gate: a
// non-admin caller is denied before any agent round trip.
func TestDeviceLogsBroker_NonAdminForbidden(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)
	viewerToken, err := env.jwt.GenerateToken(user.ID, user.Email, false)
	require.NoError(t, err)

	_, deviceID := env.connectAgent(t, group.ID)

	req, err := http.NewRequest(http.MethodGet, env.httpSrv.URL+"/api/v1/devices/"+deviceID.String()+"/logs", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", bearerPrefix+viewerToken)
	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}
