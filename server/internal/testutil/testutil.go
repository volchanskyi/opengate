// Package testutil provides shared test helpers for the OpenGate server test suite.
// It is intended to be imported only from _test.go files.
package testutil

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const postgresTestURLEnv = "POSTGRES_TEST_URL"

// pgOnce ensures the shared Postgres test store is provisioned at most once.
var pgOnce sync.Once

// pgTestDB is the shared Postgres store for all external test packages.
var pgTestDB *db.PostgresStore

// pgSetupErr captures any error from the one-time setup.
var pgSetupErr error

// pgSchemaName is a per-process unique schema to avoid races between parallel
// test binaries (go test runs packages concurrently).
var pgSchemaName string


// initPostgresTestDB provisions the shared Postgres test store in its own
// per-process schema so parallel package test binaries don't interfere.
func initPostgresTestDB(baseURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use a short random suffix to isolate from parallel test binaries.
	pgSchemaName = "ogt_" + uuid.New().String()[:8]

	setup, err := db.NewPostgresStore(ctx, baseURL)
	if err != nil {
		return fmt.Errorf("open base url: %w", err)
	}
	// Schema name is generated in-process (not user input). Using string
	// concatenation for DDL is safe here — it never touches external input.
	if _, err := setup.DB().ExecContext(ctx, `CREATE SCHEMA `+pgSchemaName); err != nil {
		_ = setup.Close()
		return fmt.Errorf("create schema: %w", err)
	}
	_ = setup.Close()

	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	testURL := baseURL + sep + "search_path=" + pgSchemaName
	store, err := db.NewPostgresStore(ctx, testURL)
	if err != nil {
		return fmt.Errorf("open test url: %w", err)
	}
	pgTestDB = store
	return nil
}

// truncateTestDB wipes all rows and re-seeds the Administrators group inside a
// single transaction to avoid races with concurrent test packages.
func truncateTestDB(ctx context.Context) error {
	tx, err := pgTestDB.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback on commit is no-op
	if _, err := tx.ExecContext(ctx, `
		TRUNCATE TABLE
			security_group_members,
			device_logs,
			device_hardware,
			device_updates,
			enrollment_tokens,
			amt_devices,
			audit_events,
			web_push_subscriptions,
			agent_sessions,
			devices,
			groups_,
			security_groups,
			users
		RESTART IDENTITY CASCADE`); err != nil {
		return fmt.Errorf("truncate tables: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO security_groups (id, name, description, is_system)
		VALUES ('00000000-0000-0000-0000-000000000001', 'Administrators', 'Full system access', TRUE)`); err != nil {
		return fmt.Errorf("seed administrators group: %w", err)
	}
	return tx.Commit()
}

// NewTestStore returns a Postgres-backed store for testing. The store is shared
// across all tests in the same package run — each call TRUNCATEs all tables to
// isolate state. Tests that use this helper must NOT call t.Parallel().
//
// Requires POSTGRES_TEST_URL to be set; skips the test otherwise.
func NewTestStore(t testing.TB) db.Store {
	t.Helper()

	baseURL := os.Getenv(postgresTestURLEnv)
	if baseURL == "" {
		t.Skipf("%s not set; skipping Postgres tests", postgresTestURLEnv)
	}

	pgOnce.Do(func() {
		pgSetupErr = initPostgresTestDB(baseURL)
	})
	require.NoError(t, pgSetupErr, "postgres test setup")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, truncateTestDB(ctx))
	return pgTestDB
}

// SeedUser inserts a minimal user into the store and returns it.
// The email is randomised to avoid uniqueness conflicts across parallel tests.
func SeedUser(t testing.TB, ctx context.Context, s db.Store) *db.User {
	t.Helper()
	u := &db.User{
		ID:           uuid.New(),
		Email:        "test-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "hash",
		DisplayName:  "Test User",
	}
	require.NoError(t, s.UpsertUser(ctx, u))
	return u
}

// SeedGroup inserts a group owned by ownerID into the store and returns it.
func SeedGroup(t testing.TB, ctx context.Context, s db.Store, ownerID uuid.UUID) *db.Group {
	t.Helper()
	g := &db.Group{
		ID:      uuid.New(),
		Name:    "group-" + uuid.New().String()[:8],
		OwnerID: ownerID,
	}
	require.NoError(t, s.CreateGroup(ctx, g))
	return g
}

// SeedDevice inserts an offline device belonging to groupID into the store and returns it.
func SeedDevice(t testing.TB, ctx context.Context, s db.Store, groupID uuid.UUID) *db.Device {
	t.Helper()
	d := &db.Device{
		ID:       uuid.New(),
		GroupID:  groupID,
		Hostname: "host-" + uuid.New().String()[:8],
		OS:       "linux",
		Status:   db.StatusOffline,
	}
	require.NoError(t, s.UpsertDevice(ctx, d))
	return d
}

// SeedAgentSession inserts an agent session for the given device and user.
func SeedAgentSession(t testing.TB, ctx context.Context, s db.Store, deviceID, userID uuid.UUID) *db.AgentSession {
	t.Helper()
	sess := &db.AgentSession{
		Token:    string(protocol.GenerateSessionToken()),
		DeviceID: deviceID,
		UserID:   userID,
	}
	require.NoError(t, s.CreateAgentSession(ctx, sess))
	return sess
}

// SeedAdminUser inserts an admin user with a real bcrypt password hash
// and adds them to the Administrators security group.
func SeedAdminUser(t testing.TB, ctx context.Context, s db.Store) (*db.User, string) {
	t.Helper()
	password := "admin-pass-" + uuid.New().String()[:8]
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)
	u := &db.User{
		ID:           uuid.New(),
		Email:        "admin-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: hash,
		DisplayName:  "Admin User",
		IsAdmin:      true,
	}
	require.NoError(t, s.UpsertUser(ctx, u))
	require.NoError(t, s.AddSecurityGroupMember(ctx, db.AdminGroupID, u.ID))
	return u, password
}

// SeedAMTDevice inserts an AMT device record into the store.
func SeedAMTDevice(t testing.TB, ctx context.Context, s db.Store) *db.AMTDevice {
	t.Helper()
	d := &db.AMTDevice{
		UUID:     uuid.New(),
		Hostname: "amt-" + uuid.New().String()[:8],
		Model:    "Intel NUC",
		Firmware: "16.1.0",
		Status:   db.StatusOffline,
		LastSeen: time.Now(),
	}
	require.NoError(t, s.UpsertAMTDevice(ctx, d))
	return d
}
