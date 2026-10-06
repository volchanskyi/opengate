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

// TrustedProxies is the set of proxies whose X-Forwarded-For is believed, by service or by range.
// An empty set trusts nobody.
type TrustedProxies struct {
	services []proxyService
	ranges   []netip.Prefix
	resolver addrNamer

	// A refusal expires sooner than a grant because a pod that has just started is not yet
	// listed against its service.
	positiveTTL time.Duration
	negativeTTL time.Duration
	// budget bounds how long a request waits on the resolver.
	budget time.Duration

	mu   sync.Mutex
	seen map[netip.Addr]trustVerdict
}

type proxyService struct {
	name      string
	namespace string
}

type trustVerdict struct {
	trusted bool
	expires time.Time
}

type addrNamer interface {
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

const (
	defaultTrustPositiveTTL = 5 * time.Minute
	defaultTrustNegativeTTL = 5 * time.Second
	defaultTrustBudget      = 500 * time.Millisecond
	// maxTrustedPeersRemembered caps the answer map, whose keys are chosen by whoever connects.
	maxTrustedPeersRemembered = 1024
)

// ParseTrustedProxies parses the configured entries, returning nil when none are named.
// An unreadable entry is an error, so a typo cannot silently empty the trusted set.
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

// parseProxyService reads "<service>.<namespace>"; the cluster zone is matched later, so any works.
func parseProxyService(entry string) (proxyService, error) {
	labels := strings.Split(entry, ".")
	if len(labels) != 2 || labels[0] == "" || labels[1] == "" {
		return proxyService{}, fmt.Errorf(
			"trusted proxy %q is neither a range nor a <service>.<namespace>", entry)
	}
	return proxyService{name: labels[0], namespace: labels[1]}, nil
}

func (t *TrustedProxies) trusts(addr netip.Addr) bool {
	if t == nil || !addr.IsValid() {
		return false
	}

	// A range answers from the address alone, so it is neither resolved nor remembered.
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

// forgetExpired drops expired answers; the caller holds the lock.
func (t *TrustedProxies) forgetExpired(now time.Time) {
	for addr, verdict := range t.seen {
		if !now.Before(verdict.expires) {
			delete(t.seen, addr)
		}
	}
}

// askTheCluster matches the resolver's names for the peer against the services; failure refuses.
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

// nameAnswersFor matches "<pod>.<service>.<namespace>.svc.<zone>"; a zone label must follow "svc".
func nameAnswersFor(name string, service proxyService) bool {
	labels := strings.Split(strings.TrimSuffix(name, "."), ".")
	for i := 2; i < len(labels)-1; i++ {
		if labels[i] == "svc" && labels[i-2] == service.name && labels[i-1] == service.namespace {
			return true
		}
	}
	return false
}
