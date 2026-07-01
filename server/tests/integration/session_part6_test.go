package integration

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"io"
	"nhooyr.io/websocket"
	"sync"
	"testing"
	"time"
)

func TestSessionLifecycle_ConcurrentSessions(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	// Connect 3 agents — each with its own group to eliminate shared-row
	// contention that caused the flaky FOREIGN KEY failures.
	type agentInfo struct {
		stream   io.ReadWriter
		deviceID uuid.UUID
	}
	agents := make([]agentInfo, 3)
	for i := range agents {
		group := testutil.SeedGroup(t, ctx, env.store, user.ID)
		stream, deviceID := env.connectAgent(t, group.ID)
		agents[i] = agentInfo{stream: stream, deviceID: deviceID}
	}

	// Wait for all to register
	for _, a := range agents {
		require.Eventually(t, func() bool {
			d, err := env.devices.Get(defaultTenantContext(), a.deviceID)
			return err == nil && d.Status == db.StatusOnline
		}, 3*time.Second, 50*time.Millisecond)
	}

	// Create sessions for all 3 simultaneously
	type sessionResult struct {
		token    string
		deviceID uuid.UUID
	}
	results := make([]sessionResult, 3)
	var wg sync.WaitGroup
	for i, a := range agents {
		wg.Add(1)
		go func(i int, deviceID uuid.UUID) {
			defer wg.Done()
			r := env.createSession(t, jwtToken, deviceID, nil)
			results[i] = sessionResult{token: r.Token, deviceID: deviceID}
		}(i, a.deviceID)
	}
	wg.Wait()

	// All 3 sessions exist
	for _, r := range results {
		assert.Len(t, r.token, 64)
	}

	// Each relay pair exchanges data concurrently
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	var wg2 sync.WaitGroup
	for i, r := range results {
		wg2.Add(1)
		go func(i int, token string) {
			defer wg2.Done()

			agentConn := env.dialRelayWS(t, wsCtx, token, "agent", "")
			defer agentConn.Close(websocket.StatusNormalClosure, "")

			browserConn := env.dialRelayWS(t, wsCtx, token, "browser", jwtToken)
			defer browserConn.Close(websocket.StatusNormalClosure, "")

			waitForRelayWired(t, wsCtx, env.relay, protocol.SessionToken(token))

			payload := []byte("test-" + token[:8])
			require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
			_, data, err := browserConn.Read(wsCtx)
			require.NoError(t, err)
			assert.Equal(t, payload, data)
		}(i, r.token)
	}
	wg2.Wait()
}
