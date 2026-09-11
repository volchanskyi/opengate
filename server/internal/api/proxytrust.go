package api

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// TrustedProxies is the set of reverse proxies whose X-Forwarded-For this
// deployment believes.
//
// The rule it replaces believed any peer that was loopback, private or
// link-local — which inside a cluster is every pod there is. A request bucket
// is an identity, so anything that can choose its own X-Forwarded-For can mint
// a fresh allowance per request, and the old rule handed that to every
// workload sharing the cluster rather than to the one address requests actually
// enter through.
//
// An entry is one of two things:
//
//   - a Kubernetes service, written "<service>.<namespace>". The peer is
//     believed when the cluster's own resolver says that address answers for
//     that service. A service is named rather than an address because the
//     address is the controller pod's, which changes on every restart and is
//     knowable to nobody at deploy time.
//   - a range or a single address, written as a CIDR or an address. This is for
//     a venue with no cluster resolver to ask — the throwaway compose stack a
//     performance run creates and destroys inside one job, whose proxy is on a
//     bridge network that job made.
//
// Nothing configured trusts nobody, which is the right answer for a server
// reached directly.
type TrustedProxies struct {
	services []proxyService
	ranges   []netip.Prefix
	resolver addrNamer

	// How long an answer is kept. A grant is kept longer than a refusal: a
	// grant is about a pod that has to be replaced before it changes, while a
	// refusal is routinely about a pod that has only just started and is not
	// yet listed against its service — a generator whose first request arrives
	// in that gap must not be refused for the rest of the run.
	positiveTTL time.Duration
	negativeTTL time.Duration
	// How long the resolver is given. A peer is asked about once per TTL, so
	// this is paid rarely; what it bounds is a request waiting on a resolver
	// that has stopped answering.
	budget time.Duration

	mu   sync.Mutex
	seen map[netip.Addr]trustVerdict
}

// proxyService is a service and the namespace holding it.
type proxyService struct {
	name      string
	namespace string
}

// trustVerdict is one answer about one peer, and when it stops being current.
type trustVerdict struct {
	trusted bool
	expires time.Time
}

// addrNamer asks what an address is called. *net.Resolver is the shipped one;
// the cluster's resolver is the only thing that holds the answer, so it is the
// boundary a test stands in for.
type addrNamer interface {
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

const (
	defaultTrustPositiveTTL = 5 * time.Minute
	defaultTrustNegativeTTL = 5 * time.Second
	defaultTrustBudget      = 500 * time.Millisecond
	// maxTrustedPeersRemembered bounds what the answers cost to keep. A cluster
	// has far fewer peers than this; the cap is here because a map fed by
	// whoever connects is a map somebody else decides the size of.
	maxTrustedPeersRemembered = 1024
)

// ParseTrustedProxies reads the configured entries. Nil with no error means
// nothing was named, which is a deployment that believes no proxy at all.
//
// An entry that cannot be read is an error rather than a skip: a name with a
// typo in it would otherwise silently narrow the trusted set to nothing, and
// the symptom — every technician behind one allowance — appears somewhere else
// entirely.
func ParseTrustedProxies(entries []string) (*TrustedProxies, error) {
	trust := &TrustedProxies{
		positiveTTL: defaultTrustPositiveTTL,
		negativeTTL: defaultTrustNegativeTTL,
		budget:      defaultTrustBudget,
		resolver:    net.DefaultResolver,
		seen:        make(map[netip.Addr]trustVerdict),
	}

	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		switch {
		case strings.Contains(entry, "/"):
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("trusted proxy %q is not a range: %w", entry, err)
			}
			trust.ranges = append(trust.ranges, prefix.Masked())
		case isAddress(entry):
			addr := netip.MustParseAddr(entry)
			trust.ranges = append(trust.ranges, netip.PrefixFrom(addr, addr.BitLen()))
		default:
			service, err := parseProxyService(entry)
			if err != nil {
				return nil, err
			}
			trust.services = append(trust.services, service)
		}
	}

	if len(trust.services) == 0 && len(trust.ranges) == 0 {
		return nil, nil
	}
	return trust, nil
}

func isAddress(entry string) bool {
	_, err := netip.ParseAddr(entry)
	return err == nil
}

// parseProxyService reads "<service>.<namespace>". Exactly two labels: the rest
// of the name a cluster gives a service — the "svc" label and the cluster's own
// zone — is matched structurally rather than spelled here, so a cluster with a
// different zone needs no second home for it.
func parseProxyService(entry string) (proxyService, error) {
	labels := strings.Split(entry, ".")
	if len(labels) != 2 || labels[0] == "" || labels[1] == "" {
		return proxyService{}, fmt.Errorf(
			"trusted proxy %q is neither a range nor a <service>.<namespace>", entry)
	}
	return proxyService{name: labels[0], namespace: labels[1]}, nil
}

// trusts reports whether the peer at addr is one of the proxies this deployment
// operates. A nil receiver — nothing configured — trusts nobody.
func (t *TrustedProxies) trusts(addr netip.Addr) bool {
	if t == nil || !addr.IsValid() {
		return false
	}

	// A range is a fact about the address itself, so it is answered without
	// asking anybody and without being remembered.
	for _, prefix := range t.ranges {
		if prefix.Contains(addr) {
			return true
		}
	}
	if len(t.services) == 0 {
		return false
	}

	now := time.Now()
	if verdict, ok := t.recall(addr, now); ok {
		return verdict
	}

	trusted := t.askTheCluster(addr)
	t.remember(addr, trusted, now)
	return trusted
}

func (t *TrustedProxies) recall(addr netip.Addr, now time.Time) (bool, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	verdict, ok := t.seen[addr]
	if !ok || !now.Before(verdict.expires) {
		return false, false
	}
	return verdict.trusted, true
}

func (t *TrustedProxies) remember(addr netip.Addr, trusted bool, now time.Time) {
	ttl := t.negativeTTL
	if trusted {
		ttl = t.positiveTTL
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.seen) >= maxTrustedPeersRemembered {
		t.forgetExpired(now)
		if len(t.seen) >= maxTrustedPeersRemembered {
			t.seen = make(map[netip.Addr]trustVerdict, maxTrustedPeersRemembered)
		}
	}
	t.seen[addr] = trustVerdict{trusted: trusted, expires: now.Add(ttl)}
}

// forgetExpired drops answers that are no longer current. The caller holds the
// lock.
func (t *TrustedProxies) forgetExpired(now time.Time) {
	for addr, verdict := range t.seen {
		if !now.Before(verdict.expires) {
			delete(t.seen, addr)
		}
	}
}

// askTheCluster asks the resolver what the peer is called and reads the answer
// for a service this deployment named. A resolver that cannot answer produces a
// refusal: a question that could not be asked is never a grant.
func (t *TrustedProxies) askTheCluster(addr netip.Addr) bool {
	ctx, cancel := context.WithTimeout(context.Background(), t.budget)
	defer cancel()

	names, err := t.resolver.LookupAddr(ctx, addr.String())
	if err != nil {
		return false
	}
	for _, name := range names {
		for _, service := range t.services {
			if nameAnswersFor(name, service) {
				return true
			}
		}
	}
	return false
}

// nameAnswersFor reports whether a cluster name belongs to a service.
//
// A cluster names an endpoint "<pod>.<service>.<namespace>.svc.<zone>", where
// the leading label is the dashed address for an ordinary pod and the pod's own
// hostname for one in a set. Which pod it is was never the question, so the
// match is on the "svc" label with the service and its namespace in front of
// it, and at least one label of zone behind — a name that stops at "svc" names
// no cluster.
func nameAnswersFor(name string, service proxyService) bool {
	labels := strings.Split(strings.TrimSuffix(name, "."), ".")
	for i := 2; i < len(labels)-1; i++ {
		if labels[i] == "svc" && labels[i-2] == service.name && labels[i-1] == service.namespace {
			return true
		}
	}
	return false
}
