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
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/organization"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/testpg"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// Per-test pool caps keep many parallel tests within one Postgres instance's connections.
const (
	testMaxOpenConns = 3
	testMaxIdleConns = 1

	// maxLiveStores caps live test schemas per test binary; each uses up to ~12 transient connections.
	// Parallel binaries together need Postgres at max_connections=400.
	maxLiveStores = 16
)

// liveStoreSem bounds concurrent test stores: acquired in NewTestStore, released in t.Cleanup.
var liveStoreSem = make(chan struct{}, maxLiveStores)

// openAdminSQL returns a single-connection sql.DB for short-lived schema CREATE/DROP statements.
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

// NewTestStore returns a Postgres-backed store on a fresh, migrated per-test schema.
// The schema is dropped at cleanup, so callers may use t.Parallel.
func NewTestStore(t testing.TB) *db.PostgresStore {
	t.Helper()
	return newTestStore(t, testMaxOpenConns)
}

// NewTestStoreWithPool is NewTestStore with the pool capped at maxOpenConns connections.
func NewTestStoreWithPool(t testing.TB, maxOpenConns int) *db.PostgresStore {
	t.Helper()
	return newTestStore(t, maxOpenConns)
}

func newTestStore(t testing.TB, maxOpenConns int) *db.PostgresStore {
	t.Helper()

	pgBaseURL := testpg.BaseURL(t)

	// The cleanup is registered right after acquiring so a setup failure still releases the slot.
	// schemaName is captured by reference and stays empty if setup failed before CREATE SCHEMA.
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

	// PostgreSQL identifiers are limited to 63 bytes; "ogt_" plus 16 hex characters stays under it.
	schemaName = "ogt_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The schema identifier is generated in-process, so inlining it into the DDL is safe.
	admin, err := openAdminSQL(ctx, pgBaseURL)
	require.NoErrorf(t, err, "open admin sql for schema setup")
	_, err = admin.ExecContext(ctx, `CREATE SCHEMA `+schemaName)
	if err != nil {
		_ = admin.Close()
		require.NoErrorf(t, err, "create schema %s", schemaName)
	}
	_ = admin.Close()

	sep := "?"
	if strings.Contains(pgBaseURL, "?") {
		sep = "&"
	}
	testURL := pgBaseURL + sep + "search_path=" + schemaName
	store, err = db.NewPostgresStoreWithOptions(ctx, testURL, db.PostgresOptions{
		MaxOpenConns: maxOpenConns,
		MaxIdleConns: min(testMaxIdleConns, maxOpenConns),
	})
	require.NoErrorf(t, err, "open test store for schema %s", schemaName)

	return store
}

// NewUnmigratedDB opens a pool whose search_path names a missing schema, so table reads fail.
func NewUnmigratedDB(t testing.TB) *sql.DB {
	t.Helper()

	base := testpg.BaseURL(t)
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	pool, err := sql.Open("pgx", base+sep+"search_path=ogt_no_such_schema")
	require.NoError(t, err, "open unmigrated pool")
	pool.SetMaxOpenConns(testMaxOpenConns)
	pool.SetMaxIdleConns(testMaxIdleConns)
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

// NewTestAudit returns a Postgres-backed audit.Repository sharing the connection pool of s.
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

// NewTestSites returns a Postgres-backed device.SiteRepository.
func NewTestSites(t testing.TB, s *db.PostgresStore) device.SiteRepository {
	t.Helper()
	return device.NewPostgresSites(s.DB())
}

// NewTestHardware returns a Postgres-backed device.HardwareRepository.
func NewTestHardware(t testing.TB, s *db.PostgresStore) device.HardwareRepository {
	t.Helper()
	return device.NewPostgresHardware(s.DB())
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

// EnsureTenant inserts tenantID if absent, along with the default organization every tenant has.
func EnsureTenant(t testing.TB, ctx context.Context, s *db.PostgresStore, tenantID uuid.UUID, name string) {
	t.Helper()
	_, err := s.DB().ExecContext(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`,
		tenantID, name)
	require.NoError(t, err)
	_, err = NewTestOrganizations(t, s).EnsureDefault(dbtx.WithTenant(ctx, tenantID, false))
	require.NoError(t, err)
}

// NewTestOrganizations returns an organization.Repository over the store.
func NewTestOrganizations(t testing.TB, s *db.PostgresStore) organization.Repository {
	t.Helper()
	return organization.NewPostgresOrganizations(s.DB())
}

func tenantOrDefault(ctx context.Context, isAdmin bool) (context.Context, dbtx.Tenant) {
	if tenant, ok := dbtx.TenantFromContext(ctx); ok {
		return ctx, tenant
	}
	return dbtx.WithDefaultTenant(ctx, isAdmin), dbtx.Tenant{TenantID: dbtx.DefaultTenantID, IsAdmin: isAdmin}
}

// SeedUser inserts a minimal user via the auth.UserRepository. The email is
// randomised to avoid uniqueness conflicts across parallel tests.
func SeedUser(t testing.TB, ctx context.Context, s *db.PostgresStore) *auth.User {
	t.Helper()
	ctx, tenant := tenantOrDefault(ctx, false)
	u := &auth.User{
		ID:           uuid.New(),
		TenantID:     tenant.TenantID,
		Email:        "test-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "hash",
		DisplayName:  "Test User",
	}
	require.NoError(t, NewTestUsers(t, s).Upsert(ctx, u))
	return u
}

// SeedSite inserts a site under the tenant's own customer and returns it.
func SeedSite(t testing.TB, ctx context.Context, s *db.PostgresStore) *device.Site {
	t.Helper()
	ctx, _ = tenantOrDefault(ctx, false)
	organizationID, err := NewTestOrganizations(t, s).EnsureDefault(ctx)
	require.NoError(t, err)
	return SeedSiteIn(t, ctx, s, organizationID)
}

// SeedSiteIn inserts a site under the customer organizationID and returns it.
func SeedSiteIn(t testing.TB, ctx context.Context, s *db.PostgresStore, organizationID uuid.UUID) *device.Site {
	t.Helper()
	ctx, _ = tenantOrDefault(ctx, false)
	site := &device.Site{
		ID:             uuid.New(),
		OrganizationID: organizationID,
		Name:           "site-" + uuid.New().String()[:8],
	}
	require.NoError(t, NewTestSites(t, s).Create(ctx, site))
	return site
}

// SeedOrganization inserts another customer inside the caller's tenant and returns its id.
func SeedOrganization(t testing.TB, ctx context.Context, s *db.PostgresStore, name string) uuid.UUID {
	t.Helper()
	ctx, _ = tenantOrDefault(ctx, false)
	org := &organization.Organization{
		ID:   uuid.New(),
		Name: name + "-" + uuid.New().String()[:8],
	}
	require.NoError(t, NewTestOrganizations(t, s).Create(ctx, org))
	return org.ID
}

// SeedDevice inserts an offline device belonging to siteID into the store and returns it.
func SeedDevice(t testing.TB, ctx context.Context, s *db.PostgresStore, siteID uuid.UUID) *device.Device {
	t.Helper()
	ctx, _ = tenantOrDefault(ctx, false)
	d := &device.Device{
		ID:       uuid.New(),
		SiteID:   siteID,
		Hostname: "host-" + uuid.New().String()[:8],
		OS:       "linux",
		Status:   device.StatusOffline,
	}
	require.NoError(t, NewTestDevices(t, s).Upsert(ctx, d))
	return d
}

// SeedDeviceIn inserts an offline device under the customer organizationID.
func SeedDeviceIn(t testing.TB, ctx context.Context, s *db.PostgresStore, organizationID, siteID uuid.UUID) *device.Device {
	t.Helper()
	ctx, _ = tenantOrDefault(ctx, false)
	d := &device.Device{
		ID:             uuid.New(),
		OrganizationID: organizationID,
		SiteID:         siteID,
		Hostname:       "host-" + uuid.New().String()[:8],
		OS:             "linux",
		Status:         device.StatusOffline,
	}
	require.NoError(t, NewTestDevices(t, s).Upsert(ctx, d))
	return d
}

// SeedAgentSession inserts an agent session for the given device and user
// via the session.Repository.
func SeedAgentSession(t testing.TB, ctx context.Context, s *db.PostgresStore, deviceID, userID uuid.UUID) *session.Session {
	t.Helper()
	ctx, _ = tenantOrDefault(ctx, false)
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
	ctx, tenant := tenantOrDefault(ctx, true)
	password := "admin-pass-" + uuid.New().String()[:8]
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)
	u := &auth.User{
		ID:           uuid.New(),
		TenantID:     tenant.TenantID,
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

// SeedAMTDevice inserts an AMT connection record for deviceID via the amt.Repository.
func SeedAMTDevice(t testing.TB, ctx context.Context, s *db.PostgresStore, deviceID uuid.UUID) *db.AMTDevice {
	t.Helper()
	ctx, _ = tenantOrDefault(ctx, false)
	d := &db.AMTDevice{
		UUID:     uuid.New(),
		DeviceID: deviceID,
		Status:   db.StatusOffline,
		LastSeen: time.Now(),
	}
	require.NoError(t, NewTestAMTDevices(t, s).Upsert(ctx, d))
	return d
}
