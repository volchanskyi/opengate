package api

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Which peer a deployment believes. proxytrust_test.go is about reading the
// list it was given; this is about the answer it gives for one address.

const (
	edgeAddr      = "10.244.0.10"
	neighbourAddr = "10.244.0.99"
	edgeService   = "ingress-nginx-controller.ingress-nginx"
)

// edgeNames is what the cluster's resolver actually answers for the controller
// pod, read off staging on 2026-09-10.
var edgeNames = []string{
	"10-244-0-10.ingress-nginx-controller-admission.ingress-nginx.svc.cluster.local.",
	"10-244-0-10.ingress-nginx-controller.ingress-nginx.svc.cluster.local.",
}

func TestTrustedProxiesReadsAName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		named string
		peer  string
		is    []string
		want  bool
	}{
		{"the peer the named service answers for is the edge", edgeService, edgeAddr, edgeNames, true},
		{"a pod in the same cluster that is not that service is not the edge", edgeService, neighbourAddr,
			[]string{"10-244-0-99.some-other-service.opengate-staging.svc.cluster.local."}, false},
		{"a service of that name in another namespace is a different service", edgeService, neighbourAddr,
			[]string{"10-244-0-99.ingress-nginx-controller.someone-elses.svc.cluster.local."}, false},
		{"a name that stops at svc names no cluster", edgeService, neighbourAddr,
			[]string{"10-244-0-99.ingress-nginx-controller.ingress-nginx.svc."}, false},
		// The leading label is the pod's, and which pod it is was never the
		// question: a pod in a set is named by its own hostname.
		{"a stateful pod still answers for its service", edgeService, neighbourAddr,
			[]string{"edge-0.ingress-nginx-controller.ingress-nginx.svc.cluster.local."}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			trust := newTestTrust(t, []string{tt.named}, newFakeNamer(map[string][]string{tt.peer: tt.is}))
			assert.Equal(t, tt.want, trust.trusts(netip.MustParseAddr(tt.peer)))
		})
	}
}

func TestTrustedProxiesRefusesWhereItCannotSayYes(t *testing.T) {
	t.Parallel()

	t.Run("nothing configured believes nobody, private peer included", func(t *testing.T) {
		t.Parallel()
		var trust *TrustedProxies
		assert.False(t, trust.trusts(netip.MustParseAddr(edgeAddr)),
			"anything inside the cluster used to be believed; only a named service is")
	})

	t.Run("a resolver that cannot answer is not a yes", func(t *testing.T) {
		t.Parallel()
		namer := newFakeNamer(map[string][]string{edgeAddr: edgeNames})
		namer.err = errors.New("cluster dns is down")
		trust := newTestTrust(t, []string{edgeService}, namer)
		assert.False(t, trust.trusts(netip.MustParseAddr(edgeAddr)),
			"a decision that cannot be made is the refusal, never the grant")
	})
}

func TestTrustedProxiesReadsARangeWithoutAsking(t *testing.T) {
	t.Parallel()
	namer := newFakeNamer(nil)
	trust := newTestTrust(t, []string{"172.18.0.0/16"}, namer)

	assert.True(t, trust.trusts(netip.MustParseAddr("172.18.0.5")))
	assert.False(t, trust.trusts(netip.MustParseAddr("172.19.0.5")))
	assert.Equal(t, 0, namer.callsFor("172.18.0.5"),
		"a range is a fact about the address, so there is nothing to ask")
}
