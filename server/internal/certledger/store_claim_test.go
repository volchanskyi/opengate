package certledger

import (
	"context"
	"crypto/x509"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func ledgerOver(t *testing.T) (*Store, *db.PostgresStore) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return New(store.DB(), slog.New(slog.NewTextHandler(io.Discard, nil))), store
}

// adminScope is the scope enrolment claims under: every tenant's devices are visible to it.
func adminScope() context.Context {
	return dbtx.WithDefaultTenant(context.Background(), true)
}

func TestClaimRecordsACertificateWhoseNumberNothingHolds(t *testing.T) {
	t.Parallel()
	s, _ := ledgerOver(t)
	id := uuid.New()
	c := leaf(t, id, t0)

	entry, claimed, err := s.Claim(adminScope(), c, issuerA, t0)
	require.NoError(t, err)

	require.True(t, claimed)
	assert.Equal(t, id, entry.DeviceID)
	assert.Equal(t, SourceEnroll, entry.Source)
	assert.Equal(t, 1, rowCount(t, s))
	assert.True(t, s.HasDevice(id), "a claimed number is indexed at once")
	_, found := s.Lookup(FingerprintOf(c.Raw))
	assert.True(t, found)
}

func TestClaimRefusesANumberADeviceInAnyTenantHolds(t *testing.T) {
	t.Parallel()
	s, store := ledgerOver(t)
	_, held := deviceInNewTenant(t, store, "other")

	assert.False(t, claims(t, adminScope(), s, leaf(t, held, t0)))
	assert.Equal(t, 0, rowCount(t, s))
	assert.False(t, s.HasDevice(held))
}

func TestClaimRefusesANumberDeletedInAnyTenant(t *testing.T) {
	t.Parallel()
	s, store := ledgerOver(t)
	deleted := uuid.New()
	require.NoError(t, lifecycle.NewTombstoneStore(store.DB()).
		TombstoneDevice(adminScope(), uuid.New(), deleted, nil))

	assert.False(t, claims(t, adminScope(), s, leaf(t, deleted, t0)))
	assert.Equal(t, 0, rowCount(t, s))
}

func TestClaimRefusesANumberAnEarlierCertificateHolds(t *testing.T) {
	t.Parallel()
	s, _ := ledgerOver(t)
	id := uuid.New()
	record(t, s, leaf(t, id, t0), issuerA, SourceRenew)

	assert.False(t, claims(t, adminScope(), s, leaf(t, id, t0.Add(1))))
	assert.Equal(t, 1, rowCount(t, s))
}

func TestClaimNeedsATenantScope(t *testing.T) {
	t.Parallel()
	s, _ := ledgerOver(t)

	_, claimed, err := s.Claim(context.Background(), leaf(t, uuid.New(), t0), issuerA, t0)
	require.ErrorIs(t, err, dbtx.ErrTenantRequired)

	assert.False(t, claimed)
	assert.Equal(t, 0, rowCount(t, s))
}

func TestClaimUnderACustomersScopeReadsOnlyThatCustomersDevices(t *testing.T) {
	t.Parallel()
	s, store := ledgerOver(t)
	mine, ownDevice := deviceInNewTenant(t, store, "mine")
	_, othersDevice := deviceInNewTenant(t, store, "other")

	assert.False(t, claims(t, mine, s, leaf(t, ownDevice, t0)),
		"the customer's own device holds its number")
	assert.True(t, claims(t, mine, s, leaf(t, othersDevice, t0)),
		"another customer's device is invisible under this customer's scope")
}

// deviceInNewTenant seeds a tenant holding one device, returning the tenant's own scope and the
// device's number.
func deviceInNewTenant(t *testing.T, store *db.PostgresStore, name string) (context.Context, uuid.UUID) {
	t.Helper()
	tenant := uuid.New()
	testutil.EnsureTenant(t, context.Background(), store, tenant, name+"-"+tenant.String()[:8])
	scoped := dbtx.WithTenant(context.Background(), tenant, false)
	org, err := testutil.NewTestOrganizations(t, store).EnsureDefault(scoped)
	require.NoError(t, err)
	site := testutil.SeedSiteIn(t, scoped, store, org)
	return scoped, testutil.SeedDeviceIn(t, scoped, store, org, site.ID).ID
}

// claims reports whether claiming c under ctx went through.
func claims(t *testing.T, ctx context.Context, s *Store, c *x509.Certificate) bool {
	t.Helper()
	_, claimed, err := s.Claim(ctx, c, issuerA, t0)
	require.NoError(t, err)
	return claimed
}
