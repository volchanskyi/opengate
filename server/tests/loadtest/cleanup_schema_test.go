package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver for the seeding connection
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testpg"
)

// defaultTenant is the tenant every load-test identity lives in.
const defaultTenant = "00000000-0000-0000-0000-000000000002"

// cleanupResidue is what one load run leaves behind, as the cleanup's proof
// counts it.
type cleanupResidue struct {
	Verified             bool   `json:"verified"`
	Marker               string `json:"marker"`
	RemovedUsers         int    `json:"removed_users"`
	RemovedDevices       int    `json:"removed_devices"`
	RemovedOrganizations int    `json:"removed_organizations"`
	RemovedSites         int    `json:"removed_sites"`
	OrphanUsers          int    `json:"orphan_users"`
	OrphanDevices        int    `json:"orphan_devices"`
	OrphanOrganizations  int    `json:"orphan_organizations"`
	OrphanSites          int    `json:"orphan_sites"`
}

func TestCleanupRemovesEveryKindARunCreates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	psql, conn := migratedDatabase(t, ctx)
	seedOneRunsResidue(t, ctx, conn)

	proof := runCleanup(t, psql)

	assert.True(t, proof.Verified)
	assert.Equal(t, 2, proof.RemovedUsers, "the run's two operators")
	assert.Equal(t, 1, proof.RemovedOrganizations, "the customer the run took on")
	assert.Equal(t, 1, proof.RemovedSites, "the site under it")
	assert.Equal(t, 1, proof.RemovedDevices, "the machine the run enrolled")

	assert.Zero(t, proof.OrphanUsers)
	assert.Zero(t, proof.OrphanOrganizations)
	assert.Zero(t, proof.OrphanSites)
	assert.Zero(t, proof.OrphanDevices)

	assert.Zero(t, countRows(t, ctx, conn, "SELECT COUNT(*) FROM enrollment_tokens"))
	assert.Zero(t, countRows(t, ctx, conn, "SELECT COUNT(*) FROM agent_sessions"))

	assert.Equal(t, 1, countRows(t, ctx, conn,
		"SELECT COUNT(*) FROM users WHERE email = 'opengate-service@service.invalid'"))
	assert.Equal(t, 1, countRows(t, ctx, conn,
		"SELECT COUNT(*) FROM organizations WHERE name = 'Default Organization'"))
}

func TestCleanupOnAnAlreadyEmptyDatabaseIsNotAFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	psql, _ := migratedDatabase(t, ctx)

	proof := runCleanup(t, psql)
	assert.Zero(t, proof.RemovedUsers)
	assert.Zero(t, proof.OrphanOrganizations)
}

// migratedDatabase starts a migrated database and returns a psql command prefix plus a connection.
func migratedDatabase(t *testing.T, ctx context.Context) (psqlPrefix string, conn *sql.DB) {
	t.Helper()

	container, err := postgres.Run(ctx, testpg.PostgresImage,
		postgres.WithDatabase("opengate_cleanup"),
		postgres.WithUsername("opengate"),
		postgres.WithPassword("opengate"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err, "start the database the cleanup is held against")
	t.Cleanup(func() {
		terminate, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		assert.NoError(t, container.Terminate(terminate))
	})

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Opening the store applies the migrations.
	store, err := db.NewPostgresStoreWithOptions(ctx, url, db.PostgresOptions{MaxOpenConns: 2, MaxIdleConns: 1})
	require.NoError(t, err, "apply the migrations")
	require.NoError(t, store.Close())

	conn, err = sql.Open("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	require.NoError(t, conn.PingContext(ctx))

	// psql lives in the database's own image, so the prefix runs it through docker exec.
	prefix := filepath.Join(t.TempDir(), "psql")
	script := fmt.Sprintf("#!/bin/sh\nexec docker exec -i %s psql -U opengate -d opengate_cleanup \"$@\"\n",
		container.GetContainerID())
	require.NoError(t, os.WriteFile(prefix, []byte(script), 0o700))
	return prefix, conn
}

// seedOneRunsResidue writes one of every kind of row a load run creates, from operator accounts
// to the session a technician opened.
func seedOneRunsResidue(t *testing.T, ctx context.Context, conn *sql.DB) {
	t.Helper()

	const marker = "opengate-loadtest"
	statements := []string{
		fmt.Sprintf(`INSERT INTO users (id, email, tenant_id) VALUES
			('11111111-0000-0000-0000-000000000001', '%s-1-user-01@%s.invalid', '%s'),
			('11111111-0000-0000-0000-000000000002', '%s-1-user-02@%s.invalid', '%s'),
			('11111111-0000-0000-0000-000000000003', 'opengate-service@service.invalid', '%s')`,
			marker, marker, defaultTenant, marker, marker, defaultTenant, defaultTenant),

		fmt.Sprintf(`INSERT INTO organizations (id, tenant_id, name) VALUES
			('22222222-0000-0000-0000-000000000001', '%s', '%s-1-customer-01')`, defaultTenant, marker),

		fmt.Sprintf(`INSERT INTO sites (id, tenant_id, organization_id, name) VALUES
			('33333333-0000-0000-0000-000000000001', '%s',
			 '22222222-0000-0000-0000-000000000001', '%s-1-customer-01-site-001')`, defaultTenant, marker),

		fmt.Sprintf(`INSERT INTO devices (id, tenant_id, organization_id, site_id, hostname) VALUES
			('44444444-0000-0000-0000-000000000001', '%s',
			 '22222222-0000-0000-0000-000000000001',
			 '33333333-0000-0000-0000-000000000001', '%s-machine-0001')`, defaultTenant, marker),

		fmt.Sprintf(`INSERT INTO enrollment_tokens (id, tenant_id, token, label, created_by, expires_at) VALUES
			('55555555-0000-0000-0000-000000000001', '%s', 'token-0001', '%s-fixture-1',
			 '11111111-0000-0000-0000-000000000001', NOW() + INTERVAL '1 hour')`, defaultTenant, marker),

		fmt.Sprintf(`INSERT INTO agent_sessions (token, tenant_id, device_id, user_id) VALUES
			('session-0001', '%s', '44444444-0000-0000-0000-000000000001',
			 '11111111-0000-0000-0000-000000000002')`, defaultTenant),
	}
	for _, statement := range statements {
		_, err := conn.ExecContext(ctx, statement)
		require.NoError(t, err, "seed residue")
	}
}

// runCleanup runs the script the workflow runs, against the database the prefix
// reaches, and returns the proof it wrote.
func runCleanup(t *testing.T, psqlPrefix string) cleanupResidue {
	t.Helper()

	proofPath := filepath.Join(t.TempDir(), "cleanup.json")
	script := filepath.Join(repoRoot(t), "scripts", "loadtest-cleanup.sh")

	cmd := exec.Command(script, proofPath) // #nosec G204 -- both paths are this test's own
	cmd.Env = append(os.Environ(), "LOADTEST_PSQL="+psqlPrefix)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "cleanup failed:\n%s", output)

	raw, err := os.ReadFile(proofPath) // #nosec G304 -- written by this test, into its own temp dir
	require.NoError(t, err)

	var proof cleanupResidue
	require.NoError(t, json.Unmarshal(raw, &proof))
	return proof
}

// repoRoot is where the scripts live, from this package's directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	return root
}

func countRows(t *testing.T, ctx context.Context, conn *sql.DB, query string) int {
	t.Helper()
	var count int
	require.NoError(t, conn.QueryRowContext(ctx, query).Scan(&count))
	return count
}
