package agentapi

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Per-rule coverage: how much of the fleet each rule is actually watching.
//
// A rule that quietly evaluates on half an estate while reading as healthy is
// the failure this accounting exists to make impossible, so every device is
// exactly one thing for every rule — evaluating it (active), unable to evaluate
// it (unsupported), having stopped it for costing too much (throttled), or not
// heard from (unknown) — and together they always add up to the fleet.
//
// Those states are not the same kind of fact, so they are not stored the same
// way.
//
// Active, throttled and unknown are liveness. They are supposed to reset when
// the server loses its view of the fleet: what a rule is doing on a machine is
// only knowable while that machine is connected, and a machine that has said
// nothing since the server started is exactly what unknown means. A stored
// active would let a file server unplugged three weeks ago keep claiming it is
// being watched. Those live here, in memory, which makes a restart correct by
// construction rather than by a cleanup job.
//
// Being unable to evaluate a rule is durable. A containerized agent can never
// read the kernel's per-host pressure accounting, so that is a standing hole in
// an estate's monitoring — it belongs on a remediation list, and it has to
// answer the same after a deploy as before one. That third state is persisted,
// and this store writes through to it on a change: an insert when a machine
// newly cannot evaluate a rule, a delete when it can again. Nothing is written
// while nothing changes.

// maxRuleCoverageEntries caps the rules one device may report on. Coverage
// arrives on the wire from an agent, so its size is untrusted input; a device
// can only ever be pushed a ruleset, and no ruleset approaches this, so a report
// past the cap is a misbehaving or compromised agent rather than a large estate.
const maxRuleCoverageEntries = 64

// RuleCoverageCounts is one rule's fleet split. Active + Unsupported +
// Throttled + Unknown equals the fleet size the counts were taken against.
type RuleCoverageCounts struct {
	// Active counts devices evaluating the rule.
	Active int
	// Throttled counts devices that stopped evaluating the rule because it cost
	// them more than its allowance. Unlike unsupported, this says the rule was
	// written wrong rather than that the host is short of a reading — one
	// machine reporting it is what a staged rollout is watching for.
	Throttled int
	// Unsupported counts devices that cannot evaluate it — the metric is outside
	// the vocabulary, the predicate outside the grammar's bounds, or the host
	// cannot take the reading at all. A permanent gap, stated rather than hidden.
	Unsupported int
	// Unknown counts devices that have reported nothing: offline, never
	// connected, or connected but not yet through a first summary.
	Unknown int
}

// ByState renders the split under the state names the aggregate metric carries.
// All four, always: three rendered out of four would make a rule look like it
// was watching a smaller estate than it is, which is the exact failure this
// accounting exists to make impossible.
func (c RuleCoverageCounts) ByState() map[string]int {
	return map[string]int{
		appmetrics.CoverageActive:      c.Active,
		appmetrics.CoverageThrottled:   c.Throttled,
		appmetrics.CoverageUnsupported: c.Unsupported,
		appmetrics.CoverageUnknown:     c.Unknown,
	}
}

// RuleCoverageDelta is what changed about one device's coverage. It is what the
// caller has to persist, and it is empty whenever a report says the same thing
// the last one did — which is what keeps steady state at zero writes.
type RuleCoverageDelta struct {
	// Recorded is how many rule states the report produced.
	Recorded int
	// NowUnsupported names the rules this device has newly become unable to
	// evaluate.
	NowUnsupported []string
	// NowActive names the rules it can evaluate again, whose stored rows should
	// be deleted rather than flipped to an active state.
	NowActive []string
}

// RuleCoverageStore holds what every connected device last reported about every
// rule. It is safe for concurrent use: agent read loops write, readers aggregate.
type RuleCoverageStore struct {
	mu       sync.RWMutex
	byDevice map[protocol.DeviceID]map[string]protocol.RuleCoverageState
}

// NewRuleCoverageStore returns an empty store.
func NewRuleCoverageStore() *RuleCoverageStore {
	return &RuleCoverageStore{
		byDevice: make(map[protocol.DeviceID]map[string]protocol.RuleCoverageState),
	}
}

// Report records what one device says about its rules, replacing whatever it
// said before — the latest report is the whole answer for that device, so a rule
// it no longer evaluates stops being counted rather than lingering as a stale
// active. Rule ids are sanitized and the set is capped, because this is agent
// input. Reports nothing and returns 0 when no entry survives that filter.
func (s *RuleCoverageStore) Report(device protocol.DeviceID, entries []protocol.RuleCoverage) RuleCoverageDelta {
	if s == nil {
		return RuleCoverageDelta{}
	}
	states := make(map[string]protocol.RuleCoverageState, len(entries))
	for _, entry := range entries {
		if len(states) >= maxRuleCoverageEntries {
			break
		}
		ruleID := sanitizeAlertRuleID(entry.RuleID)
		if ruleID == "" {
			continue
		}
		switch entry.State {
		case protocol.RuleCoverageActive, protocol.RuleCoverageUnsupported, protocol.RuleCoverageThrottled:
			states[ruleID] = entry.State
		default:
			// A state this server does not understand is not counted as either,
			// which leaves the device unknown for that rule — the honest answer
			// when the report cannot be read.
		}
	}
	if len(states) == 0 {
		return RuleCoverageDelta{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delta := diffCoverage(s.byDevice[device], states)
	s.byDevice[device] = states
	return delta
}

// diffCoverage names the rules whose durable state changed between what a device
// said before and what it says now. A rule it has stopped reporting altogether
// counts as evaluable again: there is no stored active, so the honest move is to
// drop the row rather than leave a claim nobody is making any more.
func diffCoverage(before, now map[string]protocol.RuleCoverageState) RuleCoverageDelta {
	delta := RuleCoverageDelta{Recorded: len(now)}
	for ruleID, state := range now {
		if state == protocol.RuleCoverageUnsupported && before[ruleID] != protocol.RuleCoverageUnsupported {
			delta.NowUnsupported = append(delta.NowUnsupported, ruleID)
		}
		if state != protocol.RuleCoverageUnsupported && before[ruleID] == protocol.RuleCoverageUnsupported {
			delta.NowActive = append(delta.NowActive, ruleID)
		}
	}
	for ruleID, state := range before {
		if state == protocol.RuleCoverageUnsupported {
			if _, still := now[ruleID]; !still {
				delta.NowActive = append(delta.NowActive, ruleID)
			}
		}
	}
	sort.Strings(delta.NowUnsupported)
	sort.Strings(delta.NowActive)
	return delta
}

// Forget drops everything a device reported. A machine that disconnects does not
// vanish from the accounting — it becomes unknown, which is exactly what it is.
func (s *RuleCoverageStore) Forget(device protocol.DeviceID) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byDevice, device)
}

// Aggregate returns the fleet split per rule, against a fleet of fleetSize
// devices. Unknown is the devices that reported nothing, so a fleet count read
// while more devices than it names are connected yields zero unknown rather than
// a negative one.
func (s *RuleCoverageStore) Aggregate(fleetSize int, unsupported map[string]int) map[string]RuleCoverageCounts {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]RuleCoverageCounts)
	// The unsupported half is not read from memory. A machine currently
	// reporting that it cannot evaluate a rule is already counted by the
	// persisted rows, and counting it here as well would count it twice.
	for _, states := range s.byDevice {
		for ruleID, state := range states {
			entry := counts[ruleID]
			switch state {
			case protocol.RuleCoverageThrottled:
				entry.Throttled++
			case protocol.RuleCoverageUnsupported:
				// Counted from the persisted rows below, never from here.
			default:
				entry.Active++
			}
			counts[ruleID] = entry
		}
	}
	for ruleID, count := range unsupported {
		entry := counts[ruleID]
		entry.Unsupported = count
		counts[ruleID] = entry
	}
	for ruleID, entry := range counts {
		if unknown := fleetSize - entry.Active - entry.Unsupported - entry.Throttled; unknown > 0 {
			entry.Unknown = unknown
			counts[ruleID] = entry
		}
	}
	return counts
}

// RuleCoverage returns the fleet split per rule for one customer, against a
// fleet of fleetSize devices. The unsupported half is read from storage, so a
// machine that cannot evaluate a rule stays counted while it is offline and
// across a restart; the rest comes from what is connected right now.
func (s *AgentServer) RuleCoverage(ctx context.Context, organizationID uuid.UUID, fleetSize int) map[string]RuleCoverageCounts {
	var unsupported map[string]int
	if s.ruleCoverage != nil {
		counts, err := s.ruleCoverage.CountUnsupported(ctx, organizationID)
		if err != nil {
			s.logger.Warn("read persisted rule coverage failed",
				"organization_id", organizationID, "error", err)
		} else {
			unsupported = counts
		}
	}
	return s.coverage.Aggregate(fleetSize, unsupported)
}

// FleetRuleCoverage returns the coverage split per rule across the whole
// install, under the state names the aggregate metric carries.
//
// Where [AgentServer.RuleCoverage] answers about one customer's estate, this
// answers about everything — which is the question a staged rollout is judged
// on, and the one the platform's own monitoring asks. A fleet that cannot be
// counted is reported rather than rendered as an install where every machine is
// unknown: that state is real and alarming, a store that is briefly down is not,
// and a gauge told they are the same would raise the one for the other.
//
// A deployment wired without the durable half has no fleet to measure against
// and reports nothing, rather than a split whose states do not add up.
func (s *AgentServer) FleetRuleCoverage(ctx context.Context) (map[string]map[string]int, error) {
	if s.fleetCoverage == nil {
		return nil, nil
	}
	fleetSize, unsupported, err := s.fleetCoverage.FleetCoverage(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fleet rule coverage: %w", err)
	}

	counts := s.coverage.Aggregate(fleetSize, unsupported)
	byState := make(map[string]map[string]int, len(counts))
	for ruleID, split := range counts {
		byState[ruleID] = split.ByState()
	}
	return byState, nil
}

// recordRuleCoverage stores what this agent reported about its rules and
// reports whether anything was recorded. A summary that carried only coverage
// still produced state, so its caller must not also count it as a discard.
//
// The durable third of that state is written through here, and only where it
// changed: a machine that keeps saying the same thing costs no write at all.
func (a *AgentConn) recordRuleCoverage(ctx context.Context, entries []protocol.RuleCoverage) bool {
	if a.coverage == nil || len(entries) == 0 {
		return false
	}
	delta := a.coverage.Report(a.DeviceID, entries)
	a.persistRuleCoverage(ctx, delta)
	return delta.Recorded > 0
}

// persistRuleCoverage writes the durable half of a coverage change. A failure is
// logged rather than propagated: coverage accounting must not be able to fail a
// machine's health summary, and the next change re-attempts the write.
func (a *AgentConn) persistRuleCoverage(ctx context.Context, delta RuleCoverageDelta) {
	if a.ruleCoverage == nil || (len(delta.NowUnsupported) == 0 && len(delta.NowActive) == 0) {
		return
	}
	organizationID := a.settingsScope(ctx).OrganizationID
	for _, ruleID := range delta.NowUnsupported {
		if err := a.ruleCoverage.MarkUnsupported(ctx, organizationID, a.DeviceID, ruleID); err != nil {
			a.logger.Warn("persist unsupported rule coverage failed",
				"device_id", a.DeviceID, "rule_id", ruleID, "error", err)
		}
	}
	for _, ruleID := range delta.NowActive {
		if err := a.ruleCoverage.ClearUnsupported(ctx, a.DeviceID, ruleID); err != nil {
			a.logger.Warn("clear unsupported rule coverage failed",
				"device_id", a.DeviceID, "rule_id", ruleID, "error", err)
		}
	}
}

// FleetCoverageSource counts the whole install: how many machines it has, and
// per rule how many of them cannot evaluate it.
//
// Separate from [UnsupportedCoverageStore] because it is a different question
// asked by a different caller. That one is the connection's write-through, on
// the path of every health summary; this is one read on a metrics timer. Folding
// them together would make every stand-in for the write path implement a
// fleet-wide read it never calls.
type FleetCoverageSource interface {
	// FleetCoverage returns the fleet size and, per rule, how many machines
	// cannot evaluate it. Rules nothing is blind to are absent.
	FleetCoverage(ctx context.Context) (int, map[string]int, error)
}

// UnsupportedCoverageStore persists the durable third of coverage: which
// machines cannot evaluate which rules. Presence of a record is the state, so
// there is nothing stored that can go stale into a claim that a decommissioned
// machine is being watched.
type UnsupportedCoverageStore interface {
	// MarkUnsupported records that a machine cannot evaluate a rule, keeping
	// the moment it first could not.
	MarkUnsupported(ctx context.Context, organizationID, deviceID uuid.UUID, ruleID string) error
	// ClearUnsupported records that it can again, by removing the record.
	ClearUnsupported(ctx context.Context, deviceID uuid.UUID, ruleID string) error
	// CountUnsupported returns, per rule, how many of a customer's machines
	// cannot evaluate it.
	CountUnsupported(ctx context.Context, organizationID uuid.UUID) (map[string]int, error)
}
