package db

import (
	"context"
	"database/sql"
	migratepkg "github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
	"time"
)

func beginTenantTxAsRole(t *testing.T, ctx context.Context, db *sql.DB, roleName string, orgID uuid.UUID, isAdmin bool) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET LOCAL ROLE `+sqlIdent(roleName))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx,
		`SELECT set_config('app.current_org', $1, true), set_config('app.is_admin', $2, true)`,
		orgID.String(), strconv.FormatBool(isAdmin))
	require.NoError(t, err)
	return tx
}

func openRehearsalDB(t *testing.T, ctx context.Context, dbURL string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dbURL)
	require.NoError(t, err)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	require.NoError(t, db.PingContext(ctx))
	return db
}

func runMigrationSteps(t *testing.T, dbURL string, steps int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := openRehearsalDB(t, ctx, dbURL)
	defer db.Close() //nolint:errcheck // test cleanup

	migration := newTestMigrator(t, db)
	require.NoError(t, migration.Steps(steps))
}

func assertMigrationNoChange(t *testing.T, dbURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := openRehearsalDB(t, ctx, dbURL)
	defer db.Close() //nolint:errcheck // test cleanup

	migration := newTestMigrator(t, db)
	err := migration.Up()
	require.ErrorIs(t, err, migratepkg.ErrNoChange)
}

func newTestMigrator(t *testing.T, db *sql.DB) *migratepkg.Migrate {
	t.Helper()
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	require.NoError(t, err)
	dbDriver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	require.NoError(t, err)
	migration, err := migratepkg.NewWithInstance("iofs", sourceDriver, "pgx", dbDriver)
	require.NoError(t, err)
	return migration
}

func seedPreTenancyRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	defaultUserID := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	defaultGroupID := uuid.MustParse("00000000-0000-0000-0000-000000000102")
	defaultDeviceID := uuid.MustParse("00000000-0000-0000-0000-000000000103")
	securityGroupID := uuid.MustParse("00000000-0000-0000-0000-000000000104")
	enrollmentTokenID := uuid.MustParse("00000000-0000-0000-0000-000000000105")
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck // harmless after Commit
	rehearsalExec(t, ctx, tx, `INSERT INTO users (id, email, password_hash) VALUES ($1, 'rehearsal-a@example.com', 'hash')`, defaultUserID)
	rehearsalExec(t, ctx, tx, `INSERT INTO groups_ (id, name, owner_id) VALUES ($1, 'rehearsal-a', $2)`, defaultGroupID, defaultUserID)
	rehearsalExec(t, ctx, tx, `INSERT INTO devices (id, group_id, hostname) VALUES ($1, $2, 'rehearsal-a')`, defaultDeviceID, defaultGroupID)
	rehearsalExec(t, ctx, tx, `INSERT INTO agent_sessions (token, device_id, user_id) VALUES ('session-a', $1, $2)`, defaultDeviceID, defaultUserID)
	rehearsalExec(t, ctx, tx, `INSERT INTO web_push_subscriptions (endpoint, user_id) VALUES ('https://push.example.com/a', $1)`, defaultUserID)
	rehearsalExec(t, ctx, tx, `INSERT INTO audit_events (user_id, action, target) VALUES ($1, 'login', 'session')`, defaultUserID)
	rehearsalExec(t, ctx, tx, `INSERT INTO amt_devices (uuid, hostname) VALUES ('00000000-0000-0000-0000-000000000106', 'amt-a')`)
	rehearsalExec(t, ctx, tx, `INSERT INTO enrollment_tokens (id, token, created_by, expires_at) VALUES ($1, 'enroll-a', $2, NOW() + INTERVAL '1 hour')`, enrollmentTokenID, defaultUserID)
	rehearsalExec(t, ctx, tx, `INSERT INTO security_groups (id, name) VALUES ($1, 'Rehearsal Operators')`, securityGroupID)
	rehearsalExec(t, ctx, tx, `INSERT INTO security_group_members (group_id, user_id) VALUES ($1, $2)`, securityGroupID, defaultUserID)
	rehearsalExec(t, ctx, tx, `INSERT INTO device_updates (device_id, version) VALUES ($1, '1.0.0')`, defaultDeviceID)
	rehearsalExec(t, ctx, tx, `INSERT INTO device_hardware (device_id, cpu_model) VALUES ($1, 'cpu-a')`, defaultDeviceID)
	rehearsalExec(t, ctx, tx, `INSERT INTO device_logs (device_id, timestamp, level, message) VALUES ($1, '2026-07-01T00:00:00Z', 'INFO', 'log-a')`, defaultDeviceID)
	require.NoError(t, tx.Commit())
}
