package relay_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
)

const testServerID = "srv-test"

func newRegistry(t *testing.T) relay.SessionRegistry {
	t.Helper()
	return relay.NewInProcessRegistry()
}

func TestInProcess_SaveSessionIdempotentThenDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRegistry(t)
	token := protocol.SessionToken("tok-1")

	meta := relay.SessionMeta{
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
		ExpectedSides: []relay.Side{relay.SideAgent, relay.SideBrowser},
		ServerID:      testServerID,
	}
	require.NoError(t, r.SaveSession(ctx, token, meta))

	// SaveSession is idempotent — a repeat call is a no-op.
	require.NoError(t, r.SaveSession(ctx, token, meta))

	require.NoError(t, r.DeleteSession(ctx, token))

	// DeleteSession on an already-deleted token is a no-op.
	require.NoError(t, r.DeleteSession(ctx, token))
}

func TestInProcess_PingAlwaysHealthy(t *testing.T) {
	t.Parallel()
	r := newRegistry(t)
	require.NoError(t, r.Ping(context.Background()))
}
