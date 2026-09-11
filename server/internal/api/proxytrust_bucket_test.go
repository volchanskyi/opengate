package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Which allowance a request spends, once the trust decision has been made.
// proxytrust_test.go is about which peer is believed; this is about what
// follows from believing it.

func TestExtractIPAgainstTheNamedEdge(t *testing.T) {
	t.Parallel()

	const edge = "10.244.0.10"
	const generator = "10.244.0.77"
	namer := newFakeNamer(map[string][]string{
		edge:      {"10-244-0-10.ingress-nginx-controller.ingress-nginx.svc.cluster.local."},
		generator: {"10-244-0-77.opengate-staging-loadtest-generators.opengate-staging.svc.cluster.local."},
	})
	trust := newTestTrust(t, []string{
		"ingress-nginx-controller.ingress-nginx",
		"opengate-staging-loadtest-generators.opengate-staging",
	}, namer)

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{"the edge's last hop is the client it saw", edge + ":41000", "203.0.113.1, 198.51.100.9", "198.51.100.9"},
		{"the edge's single entry is that client", edge + ":41000", "203.0.113.1", "203.0.113.1"},
		{"a generator presents one technician", generator + ":41000", "198.51.100.42", "198.51.100.42"},
		{"a pod that is neither is not believed", "10.244.0.99:41000", "198.51.100.1", "10.244.0.99"},
		{"loopback is not the edge either", "127.0.0.1:80", "203.0.113.1", "127.0.0.1"},
		{"a private peer is not the edge by virtue of being private", "10.0.0.1:8080", "203.0.113.1", "10.0.0.1"},
		{"a public peer is not the edge", "203.0.113.50:44321", "198.51.100.1", "203.0.113.50"},
		{"the edge sending nothing usable falls back to the edge", edge + ":41000", "not-an-ip", edge},
		{"the edge sending an empty header falls back to the edge", edge + ":41000", "   ", edge},
		{"no header at all is the peer", edge + ":41000", "", edge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			assert.Equal(t, tt.want, extractIP(req, trust))
		})
	}
}

func TestRateLimiterBucketsPerPresentedAddress(t *testing.T) {
	t.Parallel()

	const edge = "10.244.0.10"
	namer := newFakeNamer(map[string][]string{
		edge: {"10-244-0-10.ingress-nginx-controller.ingress-nginx.svc.cluster.local."},
	})
	trust := newTestTrust(t, []string{"ingress-nginx-controller.ingress-nginx"}, namer)

	handler := RateLimiter(1, 1, trust)(okHandler)
	send := func(remoteAddr, xff string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remoteAddr
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// The load run's own subject: two technicians presented from the edge spend
	// two allowances, which is what lets a generator offer more than one
	// address' worth of work.
	assert.Equal(t, http.StatusOK, send(edge+":41000", "198.51.100.1"))
	assert.Equal(t, http.StatusTooManyRequests, send(edge+":41000", "198.51.100.1"))
	assert.Equal(t, http.StatusOK, send(edge+":41000", "198.51.100.2"))

	// And the reason the rule is narrow: a pod that is not the edge cannot do
	// the same thing.
	assert.Equal(t, http.StatusOK, send("10.244.0.99:41000", "198.51.100.3"))
	assert.Equal(t, http.StatusTooManyRequests, send("10.244.0.99:41000", "198.51.100.4"))
}
