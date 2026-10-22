package agentapi

import (
	"context"
	"runtime"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

const costEntries = 100_000

// heapGrowth is the live heap a fill function leaves behind, after collection.
func heapGrowth(fill func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fill()
	runtime.GC()
	runtime.ReadMemStats(&after)
	return after.HeapAlloc - before.HeapAlloc
}

func TestTombstoneDenyListCostPerEntry(t *testing.T) {
	s := NewAgentServer(AgentServerConfig{Logger: testLogger()})
	denyList := heapGrowth(func() {
		for range costEntries {
			s.tombstones.Store(uuid.New(), struct{}{})
		}
	})
	perDenied := denyList / costEntries
	t.Logf("deny list: %d bytes per deleted machine", perDenied)
	assert.Less(t, perDenied, uint64(200), "a deleted machine costs a bare number")

	store := testutil.NewTestStore(t)
	_, err := store.DB().Exec(`INSERT INTO device_certificates
		  (fingerprint, device_id, issuer_hash, serial, not_before, not_after, source, issued_at)
		SELECT decode(md5(i::text) || md5((i + 1)::text), 'hex'), gen_random_uuid(),
		       decode(repeat('ab', 48), 'hex'), i::text, NOW(), NOW() + INTERVAL '90 days', 'enroll', NOW()
		  FROM generate_series(1, $1::int) AS i`, costEntries)
	require.NoError(t, err)
	ledger := certledger.New(store.DB(), testLogger())
	index := heapGrowth(func() { require.NoError(t, ledger.Warm(context.Background())) })
	perCertificate := index / costEntries
	t.Logf("certificate index: %d bytes per certificate", perCertificate)
	assert.Less(t, perCertificate, uint64(450), "admission's index stays small beside a fleet")
	runtime.KeepAlive(s)
	runtime.KeepAlive(ledger)
}
