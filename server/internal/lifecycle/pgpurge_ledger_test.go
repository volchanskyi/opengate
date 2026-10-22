package lifecycle

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// seedCertifiedDevice files a device in a tenant with two ledger rows naming it.
func seedCertifiedDevice(t *testing.T, store *db.PostgresStore, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := dbtx.WithTenant(context.Background(), tenantID, false)
	testutil.EnsureTenant(t, ctx, store, tenantID, "Tenant "+tenantID.String()[:8])
	site := testutil.SeedSite(t, ctx, store)
	device := testutil.SeedDevice(t, ctx, store, site.ID)
	for i := 0; i < 2; i++ {
		_, err := store.DB().Exec(
			`INSERT INTO device_certificates
			   (fingerprint, device_id, issuer_hash, serial, not_before, not_after, source, issued_at)
			 VALUES (decode(md5($1::text || $2::text) || md5($2::text || $1::text), 'hex'), $1::uuid,
			         decode(repeat('ab', 48), 'hex'),
			         $2::text, NOW(), NOW() + INTERVAL '90 days', 'enroll', NOW())`,
			device.ID, fmt.Sprint(i))
		require.NoError(t, err)
	}
	return device.ID
}

func ledgerRows(t *testing.T, store *db.PostgresStore, deviceID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, store.DB().QueryRow(
		`SELECT count(*) FROM device_certificates WHERE device_id = $1`, deviceID).Scan(&n))
	return n
}

func TestPurgeRemovesLedgerRows(t *testing.T) {
	store := testutil.NewTestStore(t)
	purger := NewPostgresPurger(store.DB(), nil)
	tenant, other := uuid.New(), uuid.New()
	deleted := seedCertifiedDevice(t, store, tenant)
	kept := seedCertifiedDevice(t, store, other)

	require.NoError(t, purger.DeleteDevice(context.Background(), tenant, deleted))
	assert.Zero(t, ledgerRows(t, store, deleted))
	assert.Equal(t, 2, ledgerRows(t, store, kept))

	sibling := seedCertifiedDevice(t, store, tenant)
	_, err := purger.DeleteTenantDevices(context.Background(), tenant)
	require.NoError(t, err)
	assert.Zero(t, ledgerRows(t, store, sibling))
	assert.Equal(t, 2, ledgerRows(t, store, kept), "another tenant's machines keep their certificates")
}

func TestIsDeviceNumberDeletedAcrossTenants(t *testing.T) {
	ts, ctx := newTombstoneFixture(t)
	device := uuid.New()
	deleted, err := ts.IsDeviceNumberDeleted(ctx, device)
	require.NoError(t, err)
	assert.False(t, deleted)

	require.NoError(t, ts.TombstoneDevice(ctx, uuid.New(), device, nil))
	deleted, err = ts.IsDeviceNumberDeleted(ctx, device)
	require.NoError(t, err)
	assert.True(t, deleted, "a number deleted in any tenant is never handed out again")
}
