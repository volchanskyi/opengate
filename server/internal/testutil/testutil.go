// Package testutil provides shared test helpers for the OpenGate server test suite.
// It is intended to be imported only from _test.go files.
package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver for admin connections
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

const postgresTestURLEnv = "POSTGRES_TEST_URL"

// Per-test connection pool caps. Tight enough to let many parallel tests
// share a single Postgres instance — combined with the maxLiveStores
// semaphore below, peak transient connection use stays bounded.
const (
	testMaxOpenConns = 3
	testMaxIdleConns = 1

	// maxLiveStores caps the number of NewTestStore-backed schemas that
	// are alive at once IN A SINGLE TEST BINARY. The semaphore is per-
	// process; `go test ./...` runs Postgres-using packages as separate
	// binaries concurrently (default `-p` = GOMAXPROCS). Each slot's
	// lifetime touches up to ~12 transient conns (test pool + migration
	// advisory-lock + cleanup admin + lingering pg_stat_activity entries
	// that take a few seconds to clear). With 16 slots × 2 packages ×
	// ~12 ≈ 384 conns peak, callers should run Postgres with
	// `max_connections=400` (see Makefile postgres-test-up target and
	// `.github/workflows/ci.yml`).
	maxLiveStores = 16
)

// liveStoreSem throttles concurrent test-store lifetimes (acquire on
// NewTestStore, release in t.Cleanup) so the working set fits inside
// Postgres's max_connections budget. See maxLiveStores for the sizing.
var liveStoreSem = make(chan struct{}, maxLiveStores)

// openAdminSQL returns a single-connection sql.DB for short-lived schema
// CREATE/DROP operations. Avoids the overhead of NewPostgresStore (which
// would re-run migrations and open a 25-connection pool just to issue one
// DDL statement).
func openAdminSQL(ctx context.Context, url string) (*sql.DB, error) {
	d, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	if err := d.PingContext(ctx); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

var (
	pgBaseURLOnce  sync.Once
	pgBaseURL      string
	pgBaseSetupErr error
)

// initPostgresBaseURL caches the base test database URL after a one-time
// connectivity check. Migrations are NOT run here — they are run per test
// against each test's own schema. The base URL is used only for short-lived
// CREATE/DROP SCHEMA operations via openAdminSQL.
func initPostgresBaseURL() {
	pgBaseURL = os.Getenv(postgresTestURLEnv)
	if pgBaseURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d, err := openAdminSQL(ctx, pgBaseURL)
	if err != nil {
		pgBaseSetupErr = fmt.Errorf("base postgres ping: %w", err)
		return
	}
	_ = d.Close()
}

// NewTestStore returns a Postgres-backed store backed by a fresh per-test
// schema. The schema is created on entry, migrations run against it, and
// it is dropped on test cleanup. Each test gets full isolation, so tests
// using this helper MAY call t.Parallel().
//
// Requires POSTGRES_TEST_URL to be set; skips the test otherwise.
func NewTestStore(t testing.TB) db.Store {
	t.Helper()

	pgBaseURLOnce.Do(initPostgresBaseURL)
	if pgBaseURL == "" {
		t.Skipf("%s not set; skipping Postgres tests", postgresTestURLEnv)
	}
	require.NoError(t, pgBaseSetupErr, "postgres base setup")

	// Throttle concurrent live stores to stay under Postgres max_connections.
	// Register the release via t.Cleanup IMMEDIATELY after acquiring — before
	// any require.NoError calls — so a failure during setup still releases
	// the slot. The cleanup also handles schema DROP; the schemaName is
	// captured by reference and may still be "" if setup failed before
	// CREATE SCHEMA, in which case the DROP is a no-op (IF EXISTS).
	liveStoreSem <- struct{}{}
	var (
		schemaName string
		store      *db.PostgresStore
	)
	t.Cleanup(func() {
		if store != nil {
			_ = store.Close()
		}
		if schemaName != "" {
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelCleanup()
			if cleanupAdmin, err := openAdminSQL(cleanupCtx, pgBaseURL); err == nil {
				if _, err := cleanupAdmin.ExecContext(cleanupCtx, `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`); err != nil {
					t.Logf("drop schema %s: %v", schemaName, err)
				}
				_ = cleanupAdmin.Close()
			} else {
				t.Logf("postgres cleanup connect: %v", err)
			}
		}
		<-liveStoreSem
	})

	// Per-test schema name. PostgreSQL identifiers are limited to 63 bytes;
	// 16 hex chars after "ogt_" keeps us well under that and gives a
	// collision-resistant unique name.
	schemaName = "ogt_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 1: create the schema using a single-connection admin sql.DB.
	// Identifier is generated in-process (not external input) — safe to inline.
	admin, err := openAdminSQL(ctx, pgBaseURL)
	require.NoErrorf(t, err, "open admin sql for schema setup")
	_, err = admin.ExecContext(ctx, `CREATE SCHEMA `+schemaName)
	if err != nil {
		_ = admin.Close()
		require.NoErrorf(t, err, "create schema %s", schemaName)
	}
	_ = admin.Close()

	// Step 2: open the test store with search_path scoped to the new schema
	// so migrations run against an empty target and produce a fully seeded
	// schema (incl. the Administrators row from migration 001).
	sep := "?"
	if strings.Contains(pgBaseURL, "?") {
		sep = "&"
	}
	testURL := pgBaseURL + sep + "search_path=" + schemaName
	store, err = db.NewPostgresStoreWithOptions(ctx, testURL, db.PostgresOptions{
		MaxOpenConns: testMaxOpenConns,
		MaxIdleConns: testMaxIdleConns,
	})
	require.NoErrorf(t, err, "open test store for schema %s", schemaName)

	return store
}

// NewTestAudit returns a Postgres-backed audit.Repository sharing the same
// connection pool as s. s must be the *db.PostgresStore returned by
// NewTestStore (or otherwise satisfy the db.DBProvider interface) — otherwise
// the test is skipped. The audit_events schema is owned by the db package's
// migrations.
func NewTestAudit(t testing.TB, s db.Store) audit.Repository {
	t.Helper()
	return audit.NewPostgres(extractDB(t, s, "audit"))
}

// NewTestDeviceUpdates returns a Postgres-backed updater.DeviceUpdateRepository
// sharing the same connection pool as s.
func NewTestDeviceUpdates(t testing.TB, s db.Store) updater.DeviceUpdateRepository {
	t.Helper()
	return updater.NewPostgresDeviceUpdates(extractDB(t, s, "updater.DeviceUpdate"))
}

// NewTestEnrollment returns a Postgres-backed updater.EnrollmentTokenRepository
// sharing the same connection pool as s.
func NewTestEnrollment(t testing.TB, s db.Store) updater.EnrollmentTokenRepository {
	t.Helper()
	return updater.NewPostgresEnrollment(extractDB(t, s, "updater.Enrollment"))
}

// NewTestSecurityGroups returns a Postgres-backed
// auth.SecurityGroupRepository sharing the same connection pool as s. The
// security_groups + security_group_members schemas are owned by the db
// package's migrations.
func NewTestSecurityGroups(t testing.TB, s db.Store) auth.SecurityGroupRepository {
	t.Helper()
	return auth.NewPostgresSecurityGroups(extractDB(t, s, "auth.SecurityGroup"))
}

// extractDB returns the *sql.DB behind a Postgres-backed db.Store. Tests that
// need direct DB access for module-owned repos use it; if s isn't Postgres-
// backed, the test is skipped (mirrors the audit/updater leaf-module pattern).
func extractDB(t testing.TB, s db.Store, name string) *sql.DB {
	t.Helper()
	provider, ok := s.(interface{ DB() *sql.DB })
	if !ok {
		t.Skipf("%s tests require a Postgres-backed store, got %T", name, s)
	}
	return provider.DB()
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
	sg := NewTestSecurityGroups(t, s)
	require.NoError(t, sg.AddMember(ctx, auth.AdminGroupID, u.ID))
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
