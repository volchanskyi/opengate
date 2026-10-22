package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertCertificateLedger checks the ledger's shape: a 32-byte key, a 48-byte issuer hash, and
// only the two sources a certificate is issued from.
func assertCertificateLedger(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	require.True(t, tableExists(t, ctx, db, "device_certificates"))
	assert.True(t, indexExists(t, ctx, db, "idx_device_certificates_device"))

	insert := func(fingerprint, issuer int, source string) error {
		_, err := db.ExecContext(ctx,
			`INSERT INTO device_certificates
			   (fingerprint, device_id, issuer_hash, serial, not_before, not_after, source, issued_at)
			 VALUES (decode(repeat('ab', $1), 'hex'), '00000000-0000-0000-0000-000000000301',
			         decode(repeat('cd', $2), 'hex'), '1', NOW(), NOW() + INTERVAL '90 days', $3, NOW())`,
			fingerprint, issuer, source)
		return err
	}
	assert.Error(t, insert(31, 48, "enroll"), "a fingerprint is 32 bytes")
	assert.Error(t, insert(32, 47, "enroll"), "an issuer hash is 48 bytes")
	assert.Error(t, insert(32, 48, "imported"), "a certificate is issued at enrolment or renewal")
	require.NoError(t, insert(32, 48, "enroll"))
	_, err := db.ExecContext(ctx, `DELETE FROM device_certificates`)
	require.NoError(t, err)
}

func assertCertificateLedgerDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.False(t, tableExists(t, ctx, db, "device_certificates"))
}

// assertSharedIdentityColumns checks a device row can say when two machines shared its identity,
// and starts saying they never did.
func assertSharedIdentityColumns(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.True(t, columnExists(t, ctx, db, "devices", "identity_shared_at"))
	assert.True(t, columnExists(t, ctx, db, "devices", "identity_shared_addresses"))
}

func assertSharedIdentityDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.False(t, columnExists(t, ctx, db, "devices", "identity_shared_at"))
	assert.False(t, columnExists(t, ctx, db, "devices", "identity_shared_addresses"))
}

// assertConsoleSessions checks a console session is keyed by a 32-byte hash and goes with its user.
func assertConsoleSessions(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	require.True(t, tableExists(t, ctx, db, "console_sessions"))
	assert.True(t, indexExists(t, ctx, db, "idx_console_sessions_user"))

	insert := func(idBytes int) error {
		_, err := db.ExecContext(ctx,
			`INSERT INTO console_sessions
			   (id_hash, user_id, tenant_id, email, is_admin, csrf_token, started_at, last_seen_at)
			 VALUES (decode(repeat('ab', $1), 'hex'), '00000000-0000-0000-0000-000000000401',
			         '00000000-0000-0000-0000-000000000001', 'a@b.test', false, 'c', NOW(), NOW())`,
			idBytes)
		return err
	}
	assert.Error(t, insert(31), "a session id hash is 32 bytes")
}

func assertConsoleSessionsDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.False(t, tableExists(t, ctx, db, "console_sessions"))
}
