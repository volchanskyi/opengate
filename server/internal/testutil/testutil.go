// Package testutil provides shared test helpers for the OpenGate server test suite.
// It is intended to be imported only from _test.go files.
package testutil

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver for admin connections
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/testpg"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

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

// NewTestStore returns a Postgres-backed store backed by a fresh per-test
// schema. The schema is created on entry, migrations run against it, and
// it is dropped on test cleanup. Each test gets full isolation, so tests
// using this helper MAY call t.Parallel().
//
// The backing Postgres comes from POSTGRES_TEST_URL when set; otherwise a
// throwaway container is auto-provisioned (see internal/testpg). The test
// always runs — it never skips on a missing database.
func NewTestStore(t testing.TB) *db.PostgresStore {
	t.Helper()

	pgBaseURL := testpg.BaseURL(t)

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

// NewTestAudit returns a Postgres-backed audit.Repository sharing the
// connection pool of s. The audit_events schema is owned by the db package's
// migrations.
func NewTestAudit(t testing.TB, s *db.PostgresStore) audit.Repository {
	t.Helper()
	return audit.NewPostgres(s.DB())
}

// NewTestDeviceUpdates returns a Postgres-backed updater.DeviceUpdateRepository
// sharing the connection pool of s.
func NewTestDeviceUpdates(t testing.TB, s *db.PostgresStore) updater.DeviceUpdateRepository {
	t.Helper()
	return updater.NewPostgresDeviceUpdates(s.DB())
}

// NewTestEnrollment returns a Postgres-backed updater.EnrollmentTokenRepository
// sharing the connection pool of s.
func NewTestEnrollment(t testing.TB, s *db.PostgresStore) updater.EnrollmentTokenRepository {
	t.Helper()
	return updater.NewPostgresEnrollment(s.DB())
}

// NewTestSecurityGroups returns a Postgres-backed
// auth.SecurityGroupRepository sharing the connection pool of s.
func NewTestSecurityGroups(t testing.TB, s *db.PostgresStore) auth.SecurityGroupRepository {
	t.Helper()
	return auth.NewPostgresSecurityGroups(s.DB())
}

// NewTestDevices returns a Postgres-backed device.Repository sharing the
// connection pool of s.
func NewTestDevices(t testing.TB, s *db.PostgresStore) device.Repository {
	t.Helper()
	return device.NewPostgresDevices(s.DB())
}

// NewTestGroups returns a Postgres-backed device.GroupRepository.
func NewTestGroups(t testing.TB, s *db.PostgresStore) device.GroupRepository {
	t.Helper()
	return device.NewPostgresGroups(s.DB())
}

// NewTestHardware returns a Postgres-backed device.HardwareRepository.
func NewTestHardware(t testing.TB, s *db.PostgresStore) device.HardwareRepository {
	t.Helper()
	return device.NewPostgresHardware(s.DB())
}

// NewTestLogs returns a Postgres-backed device.LogsRepository.
func NewTestLogs(t testing.TB, s *db.PostgresStore) device.LogsRepository {
	t.Helper()
	return device.NewPostgresLogs(s.DB())
}

// NewTestWebPush returns a Postgres-backed notifications.WebPushRepository
// sharing the connection pool of s.
func NewTestWebPush(t testing.TB, s *db.PostgresStore) notifications.WebPushRepository {
	t.Helper()
	return notifications.NewPostgresWebPush(s.DB())
}

// NewTestAMTDevices returns a Postgres-backed amt.Repository sharing the
// connection pool of s.
func NewTestAMTDevices(t testing.TB, s *db.PostgresStore) amt.Repository {
	t.Helper()
	return amt.NewPostgresAMTDevices(s.DB())
}

// NewTestSessions returns a Postgres-backed session.Repository sharing the
// connection pool of s.
func NewTestSessions(t testing.TB, s *db.PostgresStore) session.Repository {
	t.Helper()
	return session.NewPostgresSessions(s.DB())
}

// NewTestUsers returns a Postgres-backed auth.UserRepository sharing the
// connection pool of s.
func NewTestUsers(t testing.TB, s *db.PostgresStore) auth.UserRepository {
	t.Helper()
	return auth.NewPostgresUsers(s.DB())
}

// SeedUser inserts a minimal user via the auth.UserRepository. The email is
// randomised to avoid uniqueness conflicts across parallel tests.
func SeedUser(t testing.TB, ctx context.Context, s *db.PostgresStore) *auth.User {
	t.Helper()
	u := &auth.User{
		ID:           uuid.New(),
		Email:        "test-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "hash",
		DisplayName:  "Test User",
	}
	require.NoError(t, NewTestUsers(t, s).Upsert(ctx, u))
	return u
}

// SeedGroup inserts a group owned by ownerID into the store and returns it.
// Uses an ad-hoc device.GroupRepository over the same connection pool to
// avoid forcing every test setup to thread a repo through.
func SeedGroup(t testing.TB, ctx context.Context, s *db.PostgresStore, ownerID uuid.UUID) *device.Group {
	t.Helper()
	g := &device.Group{
		ID:      uuid.New(),
		Name:    "group-" + uuid.New().String()[:8],
		OwnerID: ownerID,
	}
	require.NoError(t, NewTestGroups(t, s).Create(ctx, g))
	return g
}

// SeedDevice inserts an offline device belonging to groupID into the store and returns it.
func SeedDevice(t testing.TB, ctx context.Context, s *db.PostgresStore, groupID uuid.UUID) *device.Device {
	t.Helper()
	d := &device.Device{
		ID:       uuid.New(),
		GroupID:  groupID,
		Hostname: "host-" + uuid.New().String()[:8],
		OS:       "linux",
		Status:   device.StatusOffline,
	}
	require.NoError(t, NewTestDevices(t, s).Upsert(ctx, d))
	return d
}

// SeedAgentSession inserts an agent session for the given device and user
// via the session.Repository.
func SeedAgentSession(t testing.TB, ctx context.Context, s *db.PostgresStore, deviceID, userID uuid.UUID) *session.Session {
	t.Helper()
	sess := &session.Session{
		Token:    string(protocol.GenerateSessionToken()),
		DeviceID: deviceID,
		UserID:   userID,
	}
	require.NoError(t, NewTestSessions(t, s).Create(ctx, sess))
	return sess
}

// SeedAdminUser inserts an admin user with a real bcrypt password hash
// and adds them to the Administrators security group.
func SeedAdminUser(t testing.TB, ctx context.Context, s *db.PostgresStore) (*auth.User, string) {
	t.Helper()
	password := "admin-pass-" + uuid.New().String()[:8]
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)
	u := &auth.User{
		ID:           uuid.New(),
		Email:        "admin-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: hash,
		DisplayName:  "Admin User",
		IsAdmin:      true,
	}
	require.NoError(t, NewTestUsers(t, s).Upsert(ctx, u))
	sg := NewTestSecurityGroups(t, s)
	require.NoError(t, sg.AddMember(ctx, auth.AdminGroupID, u.ID))
	return u, password
}

// SeedAMTDevice inserts an AMT device record via the amt.Repository.
func SeedAMTDevice(t testing.TB, ctx context.Context, s *db.PostgresStore) *db.AMTDevice {
	t.Helper()
	d := &db.AMTDevice{
		UUID:     uuid.New(),
		Hostname: "amt-" + uuid.New().String()[:8],
		Model:    "Intel NUC",
		Firmware: "16.1.0",
		Status:   db.StatusOffline,
		LastSeen: time.Now(),
	}
	require.NoError(t, NewTestAMTDevices(t, s).Upsert(ctx, d))
	return d
}
