package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
	"time"
)

func TestSessionLifecycle_AgentRejectsSession(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	result := env.createSession(t, jwtToken, deviceID, nil)

	// Read SessionRequest
	codec := &protocol.Codec{}
	_, _, err = codec.ReadFrame(stream)
	require.NoError(t, err)

	// Agent rejects session
	rejectMsg := &protocol.ControlMessage{
		Type:   protocol.MsgSessionReject,
		Token:  protocol.SessionToken(result.Token),
		Reason: "busy",
	}
	rejectPayload, err := codec.EncodeControl(rejectMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, rejectPayload))

	// Relay should have no active sessions once the reject lands.
	require.Eventually(t, func() bool {
		return env.relay.ActiveSessionCount() == 0
	}, 2*time.Second, 25*time.Millisecond, "relay session count should drop to 0 after reject")
}

func TestSessionLifecycle_MultipleSessionsSameDevice(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	_, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Create 2 sessions
	result1 := env.createSession(t, jwtToken, deviceID, nil)
	result2 := env.createSession(t, jwtToken, deviceID, nil)

	// List sessions should return 2
	sessions := env.listSessions(t, jwtToken, deviceID)
	assert.Len(t, sessions, 2)

	// Delete one
	status := env.deleteSession(t, jwtToken, result1.Token)
	assert.Equal(t, http.StatusNoContent, status)

	sessions = env.listSessions(t, jwtToken, deviceID)
	assert.Len(t, sessions, 1)

	// Delete the other
	status = env.deleteSession(t, jwtToken, result2.Token)
	assert.Equal(t, http.StatusNoContent, status)

	sessions = env.listSessions(t, jwtToken, deviceID)
	assert.Empty(t, sessions)
}
