package relay_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
)

// pubEvent is the canonical event used across the Pub/Sub tests.
var pubEvent = relay.SessionEvent{Kind: relay.EventCreated, Token: protocol.SessionToken("tok-pub"), ServerID: testServerID}

// recvEvent asserts the canonical event arrives on ch within the timeout.
func recvEvent(t *testing.T, ch <-chan relay.SessionEvent) {
	t.Helper()
	select {
	case got := <-ch:
		require.Equal(t, pubEvent, got)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive published event within timeout")
	}
}

func TestRedisRegistry_PubSubDeliversToSubscriber(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r, _ := newRedisRegistry(t)
	ch, err := r.SubscribeEvents(ctx)
	require.NoError(t, err)
	require.NoError(t, r.PublishEvent(ctx, pubEvent))
	recvEvent(t, ch)
}

func TestRedisRegistry_PubSubFansOutToAllSubscribers(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r, _ := newRedisRegistry(t)
	chA, err := r.SubscribeEvents(ctx)
	require.NoError(t, err)
	chB, err := r.SubscribeEvents(ctx)
	require.NoError(t, err)
	require.NoError(t, r.PublishEvent(ctx, pubEvent))
	recvEvent(t, chA)
	recvEvent(t, chB)
}

func TestRedisRegistry_PubSubContextCancelClosesChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	r, _ := newRedisRegistry(t)
	ch, err := r.SubscribeEvents(ctx)
	require.NoError(t, err)
	cancel()
	select {
	case _, ok := <-ch:
		require.False(t, ok, "channel must close on subscription context cancel")
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after context cancel")
	}
}

func TestRedisRegistry_PublishWithoutSubscribersIsNoOp(t *testing.T) {
	t.Parallel()
	r, _ := newRedisRegistry(t)
	require.NoError(t, r.PublishEvent(context.Background(), relay.SessionEvent{Kind: relay.EventCreated}))
}
