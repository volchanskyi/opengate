package agentapi

import (
	"sync"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Per-rule coverage: how much of the fleet each rule is actually watching.
//
// A rule that quietly evaluates on half an estate while reading as healthy is
// the failure this accounting exists to make impossible, so every device is
// exactly one of three things for every rule — evaluating it (active), unable to
// evaluate it (unsupported), or not heard from (unknown) — and the three always
// add up to the fleet.
//
// Coverage is held in memory rather than in a table because it is a liveness
// view, not a history: what a rule is doing on a machine is only knowable while
// that machine is connected, and a device that has said nothing since the server
// started is exactly what unknown means. That makes a restart correct by
// construction rather than by a cleanup job.

// maxRuleCoverageEntries caps the rules one device may report on. Coverage
// arrives on the wire from an agent, so its size is untrusted input; a device
// can only ever be pushed a ruleset, and no ruleset approaches this, so a report
// past the cap is a misbehaving or compromised agent rather than a large estate.
const maxRuleCoverageEntries = 64

// RuleCoverageCounts is one rule's fleet split. Active + Unsupported + Unknown
// equals the fleet size the counts were taken against.
type RuleCoverageCounts struct {
	// Active counts devices evaluating the rule.
	Active int
	// Unsupported counts devices that cannot evaluate it — the metric is outside
	// the vocabulary, the predicate outside the grammar's bounds, or the host
	// cannot take the reading at all. A permanent gap, stated rather than hidden.
	Unsupported int
	// Unknown counts devices that have reported nothing: offline, never
	// connected, or connected but not yet through a first summary.
	Unknown int
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
func (s *RuleCoverageStore) Report(device protocol.DeviceID, entries []protocol.RuleCoverage) int {
	if s == nil {
		return 0
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
		case protocol.RuleCoverageActive, protocol.RuleCoverageUnsupported:
			states[ruleID] = entry.State
		default:
			// A state this server does not understand is not counted as either,
			// which leaves the device unknown for that rule — the honest answer
			// when the report cannot be read.
		}
	}
	if len(states) == 0 {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.byDevice[device] = states
	return len(states)
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
func (s *RuleCoverageStore) Aggregate(fleetSize int) map[string]RuleCoverageCounts {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]RuleCoverageCounts)
	for _, states := range s.byDevice {
		for ruleID, state := range states {
			entry := counts[ruleID]
			if state == protocol.RuleCoverageUnsupported {
				entry.Unsupported++
			} else {
				entry.Active++
			}
			counts[ruleID] = entry
		}
	}
	for ruleID, entry := range counts {
		if unknown := fleetSize - entry.Active - entry.Unsupported; unknown > 0 {
			entry.Unknown = unknown
			counts[ruleID] = entry
		}
	}
	return counts
}

// RuleCoverage returns the fleet split per rule across every connected agent,
// against a fleet of fleetSize devices.
func (s *AgentServer) RuleCoverage(fleetSize int) map[string]RuleCoverageCounts {
	return s.coverage.Aggregate(fleetSize)
}

// recordRuleCoverage stores what this agent reported about its rules and
// reports whether anything was recorded. A summary that carried only coverage
// still produced state, so its caller must not also count it as a discard.
func (a *AgentConn) recordRuleCoverage(entries []protocol.RuleCoverage) bool {
	if a.coverage == nil || len(entries) == 0 {
		return false
	}
	return a.coverage.Report(a.DeviceID, entries) > 0
}
