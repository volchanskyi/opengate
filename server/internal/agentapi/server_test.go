package agentapi

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestAgentServer_ConnectedAgentCount_ZeroAtStart(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	store := testutil.NewTestStore(t)
	r := relay.NewRelay(slog.Default())

	srv := NewAgentServer(cm, store, r, &notifications.NoopNotifier{}, "", testLogger())
	assert.Equal(t, 0, srv.ConnectedAgentCount())
}

func TestAgentServer_StopsOnContextCancel(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	store := testutil.NewTestStore(t)
	r := relay.NewRelay(slog.Default())

	srv := NewAgentServer(cm, store, r, &notifications.NoopNotifier{}, "", testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	// Give it a moment to start
	cancel()

	err = <-errCh
	// Should return nil or context.Canceled
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}
