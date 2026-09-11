package api

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNamer stands in for the cluster's resolver. It is the one boundary this
// decision has: what a peer address is called is a fact only DNS holds, and no
// test host can be made to hold it.
type fakeNamer struct {
	mu    sync.Mutex
	names map[string][]string
	err   error
	calls map[string]int
}

func newFakeNamer(names map[string][]string) *fakeNamer {
	return &fakeNamer{names: names, calls: map[string]int{}}
}

func (f *fakeNamer) LookupAddr(_ context.Context, addr string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[addr]++
	if f.err != nil {
		return nil, f.err
	}
	names, ok := f.names[addr]
	if !ok {
		return nil, errors.New("no such host")
	}
	return names, nil
}

func (f *fakeNamer) callsFor(addr string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[addr]
}

func TestParseTrustedProxies(t *testing.T) {
	t.Parallel()

	t.Run("an empty list trusts nothing", func(t *testing.T) {
		t.Parallel()
		trust, err := ParseTrustedProxies(nil)
		require.NoError(t, err)
		assert.Nil(t, trust, "no entry named means no proxy is believed")
	})

	t.Run("blank entries are not entries", func(t *testing.T) {
		t.Parallel()
		trust, err := ParseTrustedProxies([]string{"  ", ""})
		require.NoError(t, err)
		assert.Nil(t, trust)
	})

	accepted := []struct {
		name  string
		entry string
	}{
		{"a service and its namespace", "ingress-nginx-controller.ingress-nginx"},
		{"a range", "172.18.0.0/16"},
		{"one address", "10.4.5.6"},
		{"one address, v6", "fd00::1"},
		{"a v6 range", "fd00::/8"},
	}
	for _, tt := range accepted {
		t.Run("accepted: "+tt.name, func(t *testing.T) {
			t.Parallel()
			trust, err := ParseTrustedProxies([]string{tt.entry})
			require.NoError(t, err)
			require.NotNil(t, trust)
		})
	}

	refused := []struct {
		name  string
		entry string
	}{
		{"a service with no namespace", "ingress-nginx-controller"},
		{"a fully qualified name, which names a zone this does not pin", "svc.ns.svc.cluster.local"},
		{"a range that is not one", "10.0.0.0/64"},
		{"prose", "the ingress"},
	}
	for _, tt := range refused {
		t.Run("refused: "+tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseTrustedProxies([]string{tt.entry})
			require.Error(t, err, "an entry nobody can act on must not be read as no entry at all")
		})
	}
}

// newTestTrust builds a configured decision pointed at a resolver that answers
// from a table. Only the resolver is substituted: the parsing, the matching and
// the remembering are the shipped ones.
func newTestTrust(t *testing.T, entries []string, namer *fakeNamer) *TrustedProxies {
	t.Helper()
	trust, err := ParseTrustedProxies(entries)
	require.NoError(t, err)
	require.NotNil(t, trust)
	trust.resolver = namer
	return trust
}
