package inventory

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// fullInventory is a machine's inventory at the agent's package cap, with services beside it.
func fullInventory(version string) []Component {
	components := make([]Component, 0, 4096+512)
	for i := range 4096 {
		components = append(components, Component{Kind: KindPackage, Name: fmt.Sprintf("lib-%04d", i), Version: version})
	}
	for i := range 512 {
		components = append(components, Component{Kind: KindService, Name: fmt.Sprintf("unit-%03d.service", i), State: "running"})
	}
	return components
}

func TestAWholeInventoryIsWrittenInFiveTrips(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	dev := seedInventoryDevice(t, ctx, store)
	counter := &testutil.StatementCounter{}
	traced := NewPostgresInventoryRepository(testutil.NewTracedDB(t, store, counter))

	require.NoError(t, traced.Replace(ctx, dev, nowSecond(), fullInventory("1.0")))

	trips := counter.Take()
	require.Len(t, trips, 5, "begin, the scope, every component in one write, the prune, commit; the first: %q",
		trips[:min(len(trips), 6)])
	assert.Len(t, listOK(t, traced, ctx, dev, 0), 4096+512)
}

func TestAComponentNamedTwiceInOneReportIsStoredOnceAsItsLastSays(t *testing.T) {
	t.Parallel()
	repo, ctx, dev := newInventoryFixture(t)

	replaceOK(t, repo, ctx, dev, nowSecond(), []Component{
		{Kind: KindPackage, Name: "openssl", Version: "3.0.13"},
		{Kind: KindService, Name: "sshd.service", State: "running"},
		{Kind: KindPackage, Name: "openssl", Version: "3.0.14"},
	})

	got := byKindName(listOK(t, repo, ctx, dev, 0))
	require.Len(t, got, 2)
	assert.Equal(t, "3.0.14", got[KindPackage+"/openssl"].Version)
}
