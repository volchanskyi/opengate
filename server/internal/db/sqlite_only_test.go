package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSQLiteOnlyStore is a typed helper for the handful of tests that need
// direct access to concrete SQLite internals (raw exec, DB(), WAL files).
// The shared dual-backend tests use the Store-typed factory in store_test.go.
func newSQLiteOnlyStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestNewSQLiteStore(t *testing.T) {
	t.Run("creates database and runs migrations", func(t *testing.T) {
		s := newSQLiteOnlyStore(t)
		require.NoError(t, s.Ping(context.Background()))
	})

	t.Run("fails on invalid path", func(t *testing.T) {
		_, err := NewSQLiteStore("/nonexistent/dir/test.db")
		assert.Error(t, err)
	})

	t.Run("opens existing database", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reopen.db")

		s1, err := NewSQLiteStore(dbPath)
		require.NoError(t, err)
		ctx := context.Background()
		u := &User{ID: uuid.New(), Email: "reopen@example.com"}
		require.NoError(t, s1.UpsertUser(ctx, u))
		require.NoError(t, s1.Close())

		s2, err := NewSQLiteStore(dbPath)
		require.NoError(t, err)
		defer s2.Close()
		got, err := s2.GetUser(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, u.Email, got.Email)
	})
}

// TestCorruptUUID exercises SQLite-only error paths by bypassing the store API
// and inserting a malformed UUID via raw SQL. Postgres's UUID type rejects
// this at insert time, so this test is SQLite-specific by nature.
func TestCorruptUUID(t *testing.T) {
	s := newSQLiteOnlyStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)
	group := seedGroup(t, ctx, s, owner.ID)

	// Insert a device row with a corrupt UUID directly via raw SQL.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices (id, group_id, hostname, os, status, last_seen, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"not-a-uuid", group.ID.String(), "corrupt-host", "linux", "offline",
		"2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	require.NoError(t, err)

	// ListDevices must return an error rather than panic when it scans the corrupt row.
	_, err = s.ListDevices(ctx, group.ID)
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound))
}

// TestCorruptTimestamp mirrors TestCorruptUUID: injects a non-parseable
// timestamp through raw SQL, which Postgres's TIMESTAMPTZ would refuse.
func TestCorruptTimestamp(t *testing.T) {
	s := newSQLiteOnlyStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)
	group := seedGroup(t, ctx, s, owner.ID)

	deviceID := uuid.New()
	// Insert a device row with a corrupt timestamp directly via raw SQL.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices (id, group_id, hostname, os, status, last_seen, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		deviceID.String(), group.ID.String(), "ts-host", "linux", "offline",
		"not-a-time", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	require.NoError(t, err)

	_, err = s.GetDevice(ctx, deviceID)
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound))
}

// TestDB covers the SQLite-specific DB() accessor used by server startup
// for raw SQL maintenance (backups, size checks, etc.).
func TestDB(t *testing.T) {
	s := newSQLiteOnlyStore(t)
	assert.NotNil(t, s.DB(), "DB() should return underlying *sql.DB")
}

// TestWALMode verifies that WAL journaling is enabled — a SQLite-specific
// optimization that allows concurrent readers during writes.
func TestWALMode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wal.db")
	s, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer s.Close()

	// WAL file should be created after first write
	ctx := context.Background()
	require.NoError(t, s.UpsertUser(ctx, &User{ID: uuid.New(), Email: "wal@test.com"}))

	_, err = os.Stat(dbPath + "-wal")
	assert.NoError(t, err, "WAL file should exist")
}
