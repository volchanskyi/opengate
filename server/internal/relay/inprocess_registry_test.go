package relay_test

import (
	"context"
	"errors"
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

func TestInProcess_ClaimAffinity_FirstClaimWins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRegistry(t)
	token := protocol.SessionToken("tok-1")

	owner, err := r.ClaimAffinity(ctx, token, testServerID, 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, testServerID, owner)
}

func TestInProcess_ClaimAffinity_RepeatedClaimByOwnerReturnsSelf(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRegistry(t)
	token := protocol.SessionToken("tok-2")

	_, err := r.ClaimAffinity(ctx, token, testServerID, 30*time.Second)
	require.NoError(t, err)

	owner, err := r.ClaimAffinity(ctx, token, testServerID, 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, testServerID, owner)
}

func TestInProcess_ClaimAffinity_ConflictingServerReturnsExistingOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRegistry(t)
	token := protocol.SessionToken("tok-3")

	_, err := r.ClaimAffinity(ctx, token, "srv-a", 30*time.Second)
	require.NoError(t, err)

	owner, err := r.ClaimAffinity(ctx, token, "srv-b", 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, "srv-a", owner, "second claim must see first claim's serverID")
}

func TestInProcess_LookupOwner_UnknownToken(t *testing.T) {
	t.Parallel()
	r := newRegistry(t)

	_, err := r.LookupOwner(context.Background(), protocol.SessionToken("unknown"))
	require.ErrorIs(t, err, relay.ErrRegistryNotFound)
}

func TestInProcess_LookupOwner_AfterClaim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRegistry(t)
	token := protocol.SessionToken("tok-4")

	_, err := r.ClaimAffinity(ctx, token, testServerID, 30*time.Second)
	require.NoError(t, err)

	owner, err := r.LookupOwner(ctx, token)
	require.NoError(t, err)
	require.Equal(t, testServerID, owner)
}

func TestInProcess_SaveAndDeleteSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRegistry(t)
	token := protocol.SessionToken("tok-5")

	meta := relay.SessionMeta{
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
		ExpectedSides: []relay.Side{relay.SideAgent, relay.SideBrowser},
		ServerID:      testServerID,
	}
	require.NoError(t, r.SaveSession(ctx, token, meta))

	// SaveSession is idempotent.
	require.NoError(t, r.SaveSession(ctx, token, meta))

	require.NoError(t, r.DeleteSession(ctx, token))

	// LookupOwner reports not-found after Delete.
	_, err := r.LookupOwner(ctx, token)
	require.ErrorIs(t, err, relay.ErrRegistryNotFound)

	// DeleteSession on an already-deleted token is a no-op.
	require.NoError(t, r.DeleteSession(ctx, token))
}

func TestInProcess_PublishAndSubscribe(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := newRegistry(t)

	ch, err := r.SubscribeEvents(ctx)
	require.NoError(t, err)

	evt := relay.SessionEvent{
		Kind:     relay.EventCreated,
		Token:    protocol.SessionToken("tok-pub"),
		ServerID: testServerID,
	}
	require.NoError(t, r.PublishEvent(ctx, evt))

	select {
	case got := <-ch:
		require.Equal(t, evt, got)
	case <-time.After(time.Second):
		t.Fatal("did not receive published event within timeout")
	}
}

func TestInProcess_SubscribeFanOutsToMultipleSubscribers(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := newRegistry(t)

	chA, err := r.SubscribeEvents(ctx)
	require.NoError(t, err)
	chB, err := r.SubscribeEvents(ctx)
	require.NoError(t, err)

	evt := relay.SessionEvent{
		Kind:  relay.EventEnded,
		Token: protocol.SessionToken("tok-fan"),
	}
	require.NoError(t, r.PublishEvent(ctx, evt))

	for _, ch := range []<-chan relay.SessionEvent{chA, chB} {
		select {
		case got := <-ch:
			require.Equal(t, evt, got)
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive event")
		}
	}
}

func TestInProcess_SubscribeContextCancellationClosesChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	r := newRegistry(t)

	ch, err := r.SubscribeEvents(ctx)
	require.NoError(t, err)

	cancel()

	// After cancel, the channel must close so the consumer can exit cleanly.
	select {
	case _, ok := <-ch:
		require.False(t, ok, "channel must close on subscription context cancel")
	case <-time.After(time.Second):
		t.Fatal("channel did not close after context cancel")
	}
}

func TestInProcess_PublishWithoutSubscribers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRegistry(t)

	// Publishing with no subscribers must not block or error.
	err := r.PublishEvent(ctx, relay.SessionEvent{Kind: relay.EventCreated})
	require.NoError(t, err)
}

func TestInProcess_EmptyServerIDRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRegistry(t)

	_, err := r.ClaimAffinity(ctx, protocol.SessionToken("tok-empty"), "", 30*time.Second)
	require.Error(t, err)
	require.True(t, errors.Is(err, relay.ErrInvalidArgument), "expected ErrInvalidArgument, got %v", err)
}
