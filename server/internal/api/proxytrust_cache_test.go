package api

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What an answer costs. A peer is asked about once per answer rather than once
// per request, and what is kept is bounded in both size and age.

func TestTrustedProxiesRemembersWhatItWasTold(t *testing.T) {
	t.Parallel()

	t.Run("the same peer is asked about once, not once a request", func(t *testing.T) {
		t.Parallel()
		namer := newFakeNamer(map[string][]string{edgeAddr: edgeNames})
		trust := newTestTrust(t, []string{edgeService}, namer)
		for range 50 {
			require.True(t, trust.trusts(netip.MustParseAddr(edgeAddr)))
		}
		assert.Equal(t, 1, namer.callsFor(edgeAddr))
	})

	t.Run("a refusal is re-asked sooner than a grant is", func(t *testing.T) {
		t.Parallel()
		namer := newFakeNamer(map[string][]string{edgeAddr: edgeNames})
		trust := newTestTrust(t, []string{edgeService}, namer)
		trust.positiveTTL = time.Hour
		trust.negativeTTL = 0

		require.False(t, trust.trusts(netip.MustParseAddr(neighbourAddr)))
		require.False(t, trust.trusts(netip.MustParseAddr(neighbourAddr)))
		assert.Equal(t, 2, namer.callsFor(neighbourAddr),
			"a generator that had not joined its service yet must not be refused for the rest of the run")
	})

	t.Run("the cache cannot grow without bound", func(t *testing.T) {
		t.Parallel()
		trust := newTestTrust(t, []string{edgeService}, newFakeNamer(nil))
		for i := range maxTrustedPeersRemembered + 50 {
			trust.trusts(netip.AddrFrom4([4]byte{10, 244, byte(i / 256), byte(i % 256)}))
		}
		trust.mu.Lock()
		remembered := len(trust.seen)
		trust.mu.Unlock()
		assert.LessOrEqual(t, remembered, maxTrustedPeersRemembered)
	})
}
