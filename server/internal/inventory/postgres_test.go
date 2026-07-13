package inventory

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// seedInventoryDevice seeds a user, group, and device in the given tenant and
// returns the device id, so tests can persist inventory against a real device.
func seedInventoryDevice(t *testing.T, ctx context.Context, store *db.PostgresStore) uuid.UUID {
	t.Helper()
	owner := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, owner.ID)
	return testutil.SeedDevice(t, ctx, store, group.ID).ID
}

// newInventoryFixture builds a repository plus a default-tenant device, the
// common starting point for the single-tenant cases.
func newInventoryFixture(t *testing.T) (*PostgresInventoryRepository, context.Context, uuid.UUID) {
	t.Helper()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	return NewPostgresInventoryRepository(store.DB()), ctx, seedInventoryDevice(t, ctx, store)
}

// byKindName indexes returned components by a stable (kind,name) key.
func byKindName(components []Component) map[string]Component {
	out := make(map[string]Component, len(components))
	for _, c := range components {
		out[c.Kind+"/"+c.Name] = c
	}
	return out
}

func TestPostgresInventoryRepositoryTenantDeny(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := NewPostgresInventoryRepository(store.DB())

	orgB := uuid.New()
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	ctxB := dbtx.WithTenant(context.Background(), orgB, false)
	testutil.EnsureOrganization(t, context.Background(), store, orgB, "Tenant "+orgB.String()[:8])

	deviceA := seedInventoryDevice(t, ctxA, store)
	deviceB := seedInventoryDevice(t, ctxB, store)

	ts := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.Replace(ctxA, deviceA, ts, []Component{
		{Kind: KindPort, Name: "postgres", Proto: "tcp", Port: 5432},
	}))
	require.NoError(t, repo.Replace(ctxB, deviceB, ts, []Component{
		{Kind: KindPort, Name: "redis-server", Proto: "tcp", Port: 6379},
	}))

	got, err := repo.ListForDevice(ctxA, deviceA, 100)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "postgres", got[0].Name)

	// Tenant A must not read tenant B's inventory rows even by B's device id.
	got, err = repo.ListForDevice(ctxA, deviceB, 100)
	require.NoError(t, err)
	assert.Empty(t, got, "tenant A must not read tenant B inventory rows")

	// A missing tenant scope fails closed on both read and write.
	_, err = repo.ListForDevice(context.Background(), deviceA, 100)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	err = repo.Replace(context.Background(), deviceA, ts, []Component{{Kind: KindPackage, Name: "x"}})
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
}

func TestPostgresInventoryReplaceUpsertsAndPrunes(t *testing.T) {
	t.Parallel()
	repo, ctx, dev := newInventoryFixture(t)

	t1 := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	require.NoError(t, repo.Replace(ctx, dev, t1, []Component{
		{Kind: KindPort, Name: "postgres", Proto: "tcp", Port: 5432},
		{Kind: KindDBEngine, Name: "postgres", Version: "16.1", Port: 5432},
		{Kind: KindPackage, Name: "openssl", Version: "3.0.13"},
	}))

	first, err := repo.ListForDevice(ctx, dev, 100)
	require.NoError(t, err)
	require.Len(t, first, 3)

	// Second scan: openssl vanished, the DB engine upgraded, nginx appeared.
	t2 := t1.Add(time.Minute)
	require.NoError(t, repo.Replace(ctx, dev, t2, []Component{
		{Kind: KindPort, Name: "postgres", Proto: "tcp", Port: 5432},
		{Kind: KindDBEngine, Name: "postgres", Version: "16.2", Port: 5432},
		{Kind: KindService, Name: "nginx.service", State: "running"},
	}))

	got, err := repo.ListForDevice(ctx, dev, 100)
	require.NoError(t, err)
	idx := byKindName(got)
	require.Len(t, got, 3, "openssl must be pruned; nginx added; postgres port kept")

	_, opensslStillThere := idx[KindPackage+"/openssl"]
	assert.False(t, opensslStillThere, "vanished component must be pruned")

	engine, ok := idx[KindDBEngine+"/postgres"]
	require.True(t, ok)
	assert.Equal(t, "16.2", engine.Version, "upsert must update a changed version")

	port, ok := idx[KindPort+"/postgres"]
	require.True(t, ok)
	assert.Equal(t, t1.Unix(), port.FirstSeen.UTC().Unix(), "first_seen persists across scans")
	assert.Equal(t, t2.Unix(), port.LastSeen.UTC().Unix(), "last_seen advances to the newest scan")
}

func TestPostgresInventoryReplaceEmptyIsNoop(t *testing.T) {
	t.Parallel()
	repo, ctx, dev := newInventoryFixture(t)

	ts := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.Replace(ctx, dev, ts, []Component{
		{Kind: KindService, Name: "sshd.service", State: "running"},
	}))

	// An empty report must not wipe the last known footprint — treat it as a
	// no-op rather than a full prune, so a collector hiccup can't erase state.
	require.NoError(t, repo.Replace(ctx, dev, ts.Add(time.Minute), nil))
	got, err := repo.ListForDevice(ctx, dev, 100)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "sshd.service", got[0].Name)
}

func TestPostgresInventorySanitizesFields(t *testing.T) {
	t.Parallel()
	repo, ctx, dev := newInventoryFixture(t)

	ts := time.Now().UTC().Truncate(time.Second)
	// A control-char-bearing image (multiline / smuggled content) is defense-in-
	// depth redacted on ingest even though WS-16 already forbids secrets.
	require.NoError(t, repo.Replace(ctx, dev, ts, []Component{
		{Kind: KindContainer, Name: "cache", Runtime: "docker", Image: "redis:7\npassword=hunter2", State: "running"},
	}))

	got, err := repo.ListForDevice(ctx, dev, 100)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, redactedField, got[0].Image, "a control-char-bearing field must be redacted")

	// An unknown component kind is dropped rather than persisted or erroring.
	require.NoError(t, repo.Replace(ctx, dev, ts.Add(time.Minute), []Component{
		{Kind: KindContainer, Name: "cache", Runtime: "docker", Image: "redis:7", State: "running"},
		{Kind: "wat", Name: "bogus"},
	}))
	got, err = repo.ListForDevice(ctx, dev, 100)
	require.NoError(t, err)
	require.Len(t, got, 1, "unknown kinds must be dropped")
	assert.Equal(t, KindContainer, got[0].Kind)
}
