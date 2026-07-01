package device_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"testing"
	"time"
)

func TestPostgresLogs_UpsertQueryHasRecent(t *testing.T) {
	t.Parallel()
	devices, groups, _, logs, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	owner := seedOwner(t, ctx, store)

	g := &device.Group{ID: uuid.New(), Name: "g-" + uuid.New().String()[:8], OwnerID: owner}
	require.NoError(t, groups.Create(ctx, g))
	d := &device.Device{ID: uuid.New(), GroupID: g.ID, Hostname: "logs", OS: "linux", Status: device.StatusOffline}
	require.NoError(t, devices.Upsert(ctx, d))

	entries := []device.LogEntry{
		{Timestamp: "2026-05-20T10:00:00Z", Level: "INFO", Target: "app", Message: "started"},
		{Timestamp: "2026-05-20T10:01:00Z", Level: "WARN", Target: "app", Message: "slow request"},
		{Timestamp: "2026-05-20T10:02:00Z", Level: "ERROR", Target: "app", Message: "boom"},
	}
	require.NoError(t, logs.Upsert(ctx, d.ID, entries))

	t.Run("query unfiltered", func(t *testing.T) {
		got, total, err := logs.Query(ctx, d.ID, device.LogFilter{})
		require.NoError(t, err)
		assert.Equal(t, 3, total)
		assert.Len(t, got, 3)
	})

	t.Run("query by severity WARN+", func(t *testing.T) {
		got, total, err := logs.Query(ctx, d.ID, device.LogFilter{Level: "WARN"})
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		assert.Len(t, got, 2)
	})

	t.Run("query with search", func(t *testing.T) {
		got, total, err := logs.Query(ctx, d.ID, device.LogFilter{Search: "boom"})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Equal(t, "boom", got[0].Message)
	})

	t.Run("has recent within window", func(t *testing.T) {
		ok, err := logs.HasRecent(ctx, d.ID, 1*time.Hour)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("has recent outside window", func(t *testing.T) {
		ok, err := logs.HasRecent(ctx, d.ID, -1*time.Hour)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("upsert replaces", func(t *testing.T) {
		require.NoError(t, logs.Upsert(ctx, d.ID, []device.LogEntry{{Timestamp: "2026-05-20T11:00:00Z", Level: "INFO", Target: "x", Message: "second"}}))
		_, total, err := logs.Query(ctx, d.ID, device.LogFilter{})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
	})
}
