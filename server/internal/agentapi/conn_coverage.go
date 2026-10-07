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

// maxRuleCoverageEntries caps the rules one device may report on, since coverage is agent input.
const maxRuleCoverageEntries = 64

// RuleCoverageCounts is one rule's fleet split. Active + Unsupported +
// Throttled + Unknown equals the fleet size the counts were taken against.
type RuleCoverageCounts struct {
	// Active counts devices evaluating the rule.
	Active int
	// Throttled counts devices that stopped evaluating the rule because it cost
	// them more than its allowance.
	Throttled int
	// Unsupported counts devices that cannot evaluate it: the metric or predicate is outside
	// the supported bounds, or the host cannot take the reading.
	Unsupported int
	// Unknown counts devices that have reported nothing: offline, never
	// connected, or connected but not yet through a first summary.
	Unknown int
}

// ByState renders the split under the state names the aggregate metric carries, all four always.
func (c RuleCoverageCounts) ByState() map[string]int {
	return map[string]int{
		appmetrics.CoverageActive:      c.Active,
		appmetrics.CoverageThrottled:   c.Throttled,
		appmetrics.CoverageUnsupported: c.Unsupported,
		appmetrics.CoverageUnknown:     c.Unknown,
	}
}

// RuleCoverageDelta is what changed about one device's coverage; a repeat of the last
// report leaves both rule lists empty.
type RuleCoverageDelta struct {
	// Recorded is how many rule states the report produced.
	Recorded int
	// NowUnsupported names the rules this device has newly become unable to
	// evaluate.
	NowUnsupported []string
	// NowActive names the rules it can evaluate again, whose stored rows are deleted.
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

// Report replaces what one device said before with its latest report, sanitizing rule ids and
// capping the set. It returns an empty delta when no entry survives.
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
			// An unrecognised state leaves the device unknown for that rule.
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

// diffCoverage names the rules whose durable state changed between two reports; a rule dropped
// from the report counts as evaluable again.
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

// Forget drops everything a device reported, leaving it unknown in the accounting.
func (s *RuleCoverageStore) Forget(device protocol.DeviceID) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byDevice, device)
}

// Aggregate returns the fleet split per rule against a fleet of fleetSize devices; Unknown is
// floored at zero.
func (s *RuleCoverageStore) Aggregate(fleetSize int, unsupported map[string]int) map[string]RuleCoverageCounts {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]RuleCoverageCounts)
	for _, states := range s.byDevice {
		for ruleID, state := range states {
			entry := counts[ruleID]
			switch state {
			case protocol.RuleCoverageThrottled:
				entry.Throttled++
			case protocol.RuleCoverageUnsupported:
				// The persisted rows supply the unsupported count below.
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

// RuleCoverage returns the fleet split per rule for one customer against a fleet of fleetSize
// devices; the unsupported count is read from storage and the rest from live connections.
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

// FleetRuleCoverage returns the coverage split per rule across the whole install, by state name.
// A deployment without the durable half reports nothing, and a count failure is returned.
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

// recordRuleCoverage stores what this agent reported about its rules and reports whether
// anything was recorded, writing through only what changed.
func (a *AgentConn) recordRuleCoverage(ctx context.Context, entries []protocol.RuleCoverage) bool {
	if a.coverage == nil || len(entries) == 0 {
		return false
	}
	delta := a.coverage.Report(a.DeviceID, entries)
	a.persistRuleCoverage(ctx, delta)
	return delta.Recorded > 0
}

// persistRuleCoverage writes the durable half of a coverage change; a failure is logged so
// coverage accounting cannot fail a health summary.
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

// InstallCounter counts the whole install: its machine count and, per rule, how many of them
// cannot evaluate it.
type InstallCounter interface {
	// FleetCoverage returns the fleet size and, per rule, how many machines
	// cannot evaluate it. Rules nothing is blind to are absent.
	FleetCoverage(ctx context.Context) (int, map[string]int, error)
}

// UnsupportedCoverageStore persists which machines cannot evaluate which rules; a record's
// presence is the state.
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
