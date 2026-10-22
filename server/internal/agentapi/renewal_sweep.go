package agentapi

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

const (
	// sweepPeriod is the renewal sweep's rhythm in steady state, jittered by sweepJitter.
	sweepPeriod = 24 * time.Hour
	sweepJitter = time.Hour
	// changeoverSweepPeriod is the rhythm while the trust set names a key besides the issuer.
	changeoverSweepPeriod = 10 * time.Minute
	// offersPerSecond paces the sweep so a fleet is never offered renewal in one burst.
	offersPerSecond = 20
	// expiryWarning is how far ahead the census warns that the issuing master key runs out.
	expiryWarning = 2 * 90 * 24 * time.Hour
)

// RunCertificateMaintenance offers renewal to every due live machine on the sweep's rhythm,
// logs the authority census, and prunes the ledger, until ctx is cancelled.
func (s *AgentServer) RunCertificateMaintenance(ctx context.Context) {
	if s.renewals == nil {
		return
	}
	for {
		s.SweepRenewals(ctx)
		s.LogAuthorityCensus()
		if n, err := s.issuer.Ledger().Prune(ctx, s.cert.Now(), cert.DefaultExpiredGrace); err != nil {
			s.logger.Warn("prune certificate ledger", "error", err)
		} else if n > 0 {
			s.logger.Info("certificate ledger pruned", "rows", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.sweepWait()):
		}
	}
}

// sweepWait is the time to the next sweep: ten minutes in a changeover, a jittered day otherwise.
func (s *AgentServer) sweepWait() time.Duration {
	if s.cert.InChangeover() {
		return changeoverSweepPeriod
	}
	jitter, err := rand.Int(rand.Reader, big.NewInt(int64(2*sweepJitter)))
	if err != nil {
		return sweepPeriod
	}
	return sweepPeriod - sweepJitter + time.Duration(jitter.Int64())
}

// SweepRenewals offers renewal to every live connection whose certificate is due, at most
// offersPerSecond a second.
func (s *AgentServer) SweepRenewals(ctx context.Context) int {
	tick := time.NewTicker(time.Second / offersPerSecond)
	defer tick.Stop()
	offered := 0
	var due []*AgentConn
	now := s.cert.Now()
	s.eachLiveConn(func(ac *AgentConn) {
		if _, ok := ac.renewalReason(now); ok {
			due = append(due, ac)
		}
	})
	for _, ac := range due {
		select {
		case <-ctx.Done():
			return offered
		case <-tick.C:
		}
		ac.offerRenewal(ctx)
		offered++
	}
	return offered
}

// LogAuthorityCensus logs live machines by issuing master key, the restricted sessions, every
// recorded device by the master key of its newest certificate, and the issuer's remaining life.
func (s *AgentServer) LogAuthorityCensus() {
	if s.issuer == nil {
		return
	}
	live := map[string]int{}
	restricted := 0
	s.eachLiveConn(func(ac *AgentConn) {
		live[fmt.Sprintf("%x", ac.authority.Hash[:6])]++
		if ac.restricted {
			restricted++
		}
	})
	recorded := map[string]int{}
	var dropDates []string
	issuerHash := s.cert.IssuerHash()
	for _, c := range s.issuer.Ledger().CensusByNewestIssuer() {
		name := fmt.Sprintf("%x", c.IssuerHash[:6])
		recorded[name] = c.Devices
		if c.IssuerHash != issuerHash {
			dropDates = append(dropDates, name+"@"+c.LatestNotAfter.Add(cert.DefaultExpiredGrace).Format(time.DateOnly))
		}
	}
	s.logger.Info("authority census",
		"live_by_issuer", live, "restricted_connections", restricted,
		"recorded_by_newest_issuer", recorded, "earliest_drop", dropDates)

	left := s.cert.CACert().NotAfter.Sub(s.cert.Now())
	if left < expiryWarning {
		s.logger.Warn(fmt.Sprintf("master key expires in %d days, start the changeover", int(left.Hours()/24)))
	}
}
