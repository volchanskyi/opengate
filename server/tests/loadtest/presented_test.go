package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The server counts requests per address. A fleet that arrives from one pod
// spends one allowance between all of it, and a profile whose whole subject is
// fifteen hundred machines arriving in thirty seconds is then measuring that
// allowance rather than the server.
func TestPresentedAddressStaysInTheRangeReservedForIt(t *testing.T) {
	t.Parallel()
	benchmarking := netip.MustParsePrefix("198.18.0.0/15")

	for _, index := range []int{0, 1, 255, 256, 8191, 65535, 131071, 131072, 999999, -7} {
		addr, err := netip.ParseAddr(presentedAddress(index))
		require.NoErrorf(t, err, "index %d produced something that is not an address", index)
		assert.Truef(t, benchmarking.Contains(addr),
			"index %d presented %s, outside the range reserved for benchmark traffic — a synthetic address must never be somebody real",
			index, addr)
	}
}

func TestPresentedAddressIsOnePerMachine(t *testing.T) {
	t.Parallel()
	seen := make(map[string]int, 4096)
	for index := range 4096 {
		addr := presentedAddress(index)
		if first, clash := seen[addr]; clash {
			t.Fatalf("machines %d and %d both present %s, so they share one allowance", first, index, addr)
		}
		seen[addr] = index
	}
}

func TestPresentedAddressIsStableForOneMachine(t *testing.T) {
	t.Parallel()
	assert.Equal(t, presentedAddress(4242), presentedAddress(4242),
		"a machine that changed address between its enrolment and its filing would spend two allowances and be nobody")
}

// recordingServer answers anything and keeps the address each caller presented.
type recordingServer struct {
	mu        sync.Mutex
	presented []string
	server    *httptest.Server
}

func newRecordingServer(t *testing.T, handler http.HandlerFunc) *recordingServer {
	t.Helper()
	r := &recordingServer{}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.presented = append(r.presented, req.Header.Get("X-Forwarded-For"))
		r.mu.Unlock()
		handler(w, req)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *recordingServer) addresses() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.presented...)
}

func TestEnrolmentPresentsTheMachinesOwnAddress(t *testing.T) {
	t.Parallel()
	recorder := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	// The reply is refused, which is beside the point: what is asserted is what
	// the request carried, and it carries it whatever comes back.
	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:          recorder.server.URL,
		EnrollmentToken:  "token",
		DeviceID:         "machine-9",
		PresentedAddress: presentedAddress(9),
	})
	require.Error(t, err)

	addresses := recorder.addresses()
	require.Len(t, addresses, 1)
	assert.Equal(t, presentedAddress(9), addresses[0])
	assert.True(t, isBenchmarkAddress(addresses[0]))
}

func TestFilingPresentsTheMachinesOwnAddress(t *testing.T) {
	t.Parallel()
	recorder := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	client := NewFixtureClient(recorder.server.URL)
	built := BuiltFixture{Customers: []BuiltCustomer{{
		ID: "customer-1", Name: "Northwind", Devices: 2, SiteIDs: []string{"site-1"},
	}}}
	require.NoError(t, client.FileDevice(built, 3, 10, "device-3"))

	addresses := recorder.addresses()
	require.NotEmpty(t, addresses, "filing makes requests, and every one of them spends an allowance")
	for _, addr := range addresses {
		assert.Equal(t, presentedAddress(3), addr,
			"filing a machine is charged to the same address its arrival was")
	}
}

// A request with no address to present carries no header at all, rather than an
// empty one: an empty header read by a server that believes this peer would
// fall back to the peer anyway, and a header that says nothing is worse than no
// header for whoever reads the request later.
func TestARequestWithNothingToPresentSendsNoHeader(t *testing.T) {
	t.Parallel()
	recorder := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:         recorder.server.URL,
		EnrollmentToken: "token",
		DeviceID:        "machine-0",
	})
	require.Error(t, err)
	require.Len(t, recorder.addresses(), 1)
	assert.Empty(t, recorder.addresses()[0])
}

func isBenchmarkAddress(addr string) bool {
	parsed := net.ParseIP(addr)
	return parsed != nil && netip.MustParsePrefix("198.18.0.0/15").Contains(netip.MustParseAddr(parsed.String()))
}
