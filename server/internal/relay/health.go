package relay

import (
	"context"
	"errors"
	"time"
)

// DefaultDegradedThreshold is how long the session registry must stay
// unreachable before the relay enters degraded mode and refuses new sessions
// because a sustained outage prevents new affinity claims and could silently
// split a cross-server pair across owners. In-flight
// sessions are unaffected. InProcessRegistry never reports unhealthy, so
// single-server deployments never degrade.
const DefaultDegradedThreshold = 30 * time.Second

// registryPingTimeout bounds a single health probe so a hung registry dial can't
// stall the health-monitor loop.
const registryPingTimeout = 3 * time.Second

// ErrRegistryDegraded is returned by Register when the session registry has been
// unreachable past the degraded threshold: new sessions are refused (fail
// closed) while in-flight ones drain.
// The client should reconnect later with a fresh token.
var ErrRegistryDegraded = errors.New("session registry degraded: refusing new sessions")

// WithDegradedThreshold overrides how long the session registry must stay
// unreachable before Register fails closed (default DefaultDegradedThreshold).
// Primarily a test seam so degraded-mode behavior can be exercised without a
// real-time wait.
func WithDegradedThreshold(d time.Duration) Option {
	return func(r *Relay) {
		r.degradedThreshold = d
	}
}

// MonitorRegistryHealth periodically probes the session registry and records the
// result, driving both the opengate_registry_up gauge (RegistryUp) and the
// degraded-mode gate in Register (RegistryDegraded). It runs an immediate probe,
// then one every interval, and returns when ctx is cancelled. With
// InProcessRegistry every probe succeeds, so the relay never enters degraded
// mode (single-server deployments).
func (r *Relay) MonitorRegistryHealth(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	probe := func() {
		pingCtx, cancel := context.WithTimeout(ctx, registryPingTimeout)
		defer cancel()
		err := r.registry.Ping(pingCtx)
		r.observeRegistryHealth(err == nil, time.Now())
		if err != nil {
			r.logger.Warn("session registry health probe failed", "error", err)
		}
	}

	probe()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probe()
		}
	}
}

// observeRegistryHealth records the outcome of a single health probe. A healthy
// probe clears the unhealthy streak; an unhealthy probe starts the streak at now
// only if one is not already in progress — so RegistryDegraded measures from when
// the registry first went away, not from the latest probe.
func (r *Relay) observeRegistryHealth(healthy bool, now time.Time) {
	if healthy {
		r.registryUnhealthySince.Store(0)
		return
	}
	r.registryUnhealthySince.CompareAndSwap(0, now.UnixNano())
}

// registryDegraded reports whether the registry's unhealthy streak has exceeded
// degradedThreshold as of now. The boundary is exclusive (a streak exactly equal
// to the threshold is not yet degraded).
func (r *Relay) registryDegraded(now time.Time) bool {
	since := r.registryUnhealthySince.Load()
	if since == 0 {
		return false
	}
	return now.Sub(time.Unix(0, since)) > r.degradedThreshold
}

// RegistryDegraded reports whether the relay is currently in degraded mode (the
// session registry has been unreachable past degradedThreshold). Register uses it
// to fail closed on new sessions; it is also a convenient operational predicate.
func (r *Relay) RegistryDegraded() bool {
	return r.registryDegraded(time.Now())
}

// RegistryUp reports the registry's last observed health as true (reachable) or
// false (unreachable), for the opengate_registry_up gauge. It reflects the most
// recent MonitorRegistryHealth probe and is true before the first probe runs and
// always true with InProcessRegistry.
func (r *Relay) RegistryUp() bool {
	return r.registryUnhealthySince.Load() == 0
}
