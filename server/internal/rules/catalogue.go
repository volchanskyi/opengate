// Package rules owns what a monitoring rule may say and which machines it is watching: compiled-in
// versioned definitions, customer parameter bindings in Postgres, and per-rule rollout state.
package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const (
	// Ids travel to the agent and come back on breaches and coverage reports, so they stay short.
	maxRuleIDLen = 64
)

var groupByVocabulary = map[string]bool{
	"device":       true,
	"site":         true,
	"organization": true,
	"mount":        true,
	"metric":       true,
}

// Each entry names a body of evidence the server already collects.
var evidenceVocabulary = map[string]bool{
	"vitals":        true,
	"top_processes": true,
	"recent_logs":   true,
	"inventory":     true,
	"correlation":   true,
}

var comparatorVocabulary = map[string]protocol.AlertComparator{
	"gt":  protocol.AlertComparatorGt,
	"lt":  protocol.AlertComparatorLt,
	"gte": protocol.AlertComparatorGte,
	"lte": protocol.AlertComparatorLte,
}

// An empty name is the plain instant threshold.
var predicateVocabulary = map[string]protocol.RulePredicate{
	"":           protocol.RulePredicateInstant,
	"Instant":    protocol.RulePredicateInstant,
	"Rate":       protocol.RulePredicateRate,
	"WindowMax":  protocol.RulePredicateWindowMax,
	"WindowMean": protocol.RulePredicateWindowMean,
}

const (
	// Counting from one leaves nothing for an absent revision to be mistaken for.
	minRuleVersion = 1
	// An alert names the revision that fired, so the wire's uint32 bounds the revision.
	maxRuleVersion = uint64(math.MaxUint32)
)

// KindEvent names a rule that watches the machine's own log records.
const KindEvent = "event"

// The set is closed at the database too, so a severity outside it fails the write.
var severityVocabulary = map[string]protocol.AlertSeverity{
	"info":     protocol.AlertSeverityInfo,
	"warning":  protocol.AlertSeverityWarning,
	"critical": protocol.AlertSeverityCritical,
}

// Bounds is the range a tunable parameter may be set to; a binding outside it is refused on write.
type Bounds struct {
	Min float64 `yaml:"min" json:"min"`
	Max float64 `yaml:"max" json:"max"`
}

// Contains reports whether v is within the bounds, inclusive.
func (b Bounds) Contains(v float64) bool { return v >= b.Min && v <= b.Max }

// String renders the range for an error an operator has to act on.
func (b Bounds) String() string {
	return fmt.Sprintf("[%s, %s]",
		strconv.FormatFloat(b.Min, 'g', -1, 64), strconv.FormatFloat(b.Max, 'g', -1, 64))
}

// Term is one extra condition a rule requires at the same instant as its own.
type Term struct {
	Metric         string  `yaml:"metric" json:"metric"`
	ComparatorName string  `yaml:"comparator" json:"comparator"`
	Threshold      float64 `yaml:"threshold" json:"threshold"`
	Clear          float64 `yaml:"clear" json:"clear"`
	PredicateName  string  `yaml:"predicate" json:"predicate"`
	WindowSecs     uint32  `yaml:"window_secs" json:"window_secs"`
}

// Comparator resolves the term's comparison to the wire enum.
func (t Term) Comparator() protocol.AlertComparator { return comparatorVocabulary[t.ComparatorName] }

// Predicate resolves the term's predicate to the wire enum.
func (t Term) Predicate() protocol.RulePredicate { return predicateVocabulary[t.PredicateName] }

// Definition is one rule as the catalogue states it, immutable per (ID, Version).
type Definition struct {
	ID      string `yaml:"id" json:"id"`
	Version int    `yaml:"version" json:"version"`
	// Kind is empty for a rule about a reading, which travels to the machine; KindEvent rules match
	// phrases compiled into the machine's log reader, so the catalogue states no metric for them.
	Kind     string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Severity string `yaml:"severity" json:"severity"`
	// Summary is documentation outside the digest, so rewording it needs no version bump.
	Summary string `yaml:"summary" json:"-"`

	Metric         string  `yaml:"metric" json:"metric"`
	ComparatorName string  `yaml:"comparator" json:"comparator"`
	Threshold      float64 `yaml:"threshold" json:"threshold"`
	Clear          float64 `yaml:"clear" json:"clear"`
	SustainSecs    uint32  `yaml:"sustain_secs" json:"sustain_secs"`
	PredicateName  string  `yaml:"predicate" json:"predicate"`
	WindowSecs     uint32  `yaml:"window_secs" json:"window_secs"`
	All            []Term  `yaml:"all" json:"all"`

	// GroupBy is the key repeated firings collapse onto; a rule without one cannot be de-duplicated.
	GroupBy []string `yaml:"group_by" json:"group_by"`
	// GroupWindowSecs is how long firings on one group key stay one alert.
	GroupWindowSecs uint32   `yaml:"group_window_secs" json:"group_window_secs"`
	Evidence        []string `yaml:"evidence" json:"evidence"`
	// A device unable to read one of these metrics reports the rule unsupported.
	CoverageRequires []string `yaml:"coverage_requires" json:"coverage_requires"`
	// A parameter absent here cannot be bound at all.
	Tunable map[string]Bounds `yaml:"tunable" json:"tunable"`
}

// WatchesEvents reports whether this rule reads the machine's own words.
func (d Definition) WatchesEvents() bool { return d.Kind == KindEvent }

// WireSeverity resolves the rule's severity to the enum an alert carries.
func (d Definition) WireSeverity() protocol.AlertSeverity {
	return severityVocabulary[d.Severity]
}

// Comparator resolves the rule's comparison to the wire enum.
func (d Definition) Comparator() protocol.AlertComparator {
	return comparatorVocabulary[d.ComparatorName]
}

// Predicate resolves the rule's predicate to the wire enum.
func (d Definition) Predicate() protocol.RulePredicate { return predicateVocabulary[d.PredicateName] }

// Key is the identity a definition is immutable under.
func (d Definition) Key() string { return d.ID + "@" + strconv.Itoa(d.Version) }

// Digest fingerprints what the definition means; prose is excluded, so it changes with behavior.
func (d Definition) Digest() (string, error) {
	encoded, err := json.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("digest rule %s: %w", d.ID, err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// ShippedParam returns the definition's own value for a tunable parameter, and whether name is one.
// Only numbers on the rule are tunable; a changed metric or predicate is a different rule.
func (d Definition) ShippedParam(name string) (float64, bool) {
	switch name {
	case "threshold":
		return d.Threshold, true
	case "clear":
		return d.Clear, true
	case "sustain_secs":
		return float64(d.SustainSecs), true
	case "window_secs":
		return float64(d.WindowSecs), true
	default:
		return 0, false
	}
}

type catalogueFile struct {
	Rules []Definition `yaml:"rules"`
}

// Catalogue is the loaded, validated set of rule definitions.
type Catalogue struct {
	byID  map[string]Definition
	order []string
}

// All returns every definition, ordered by id so callers and goldens are stable.
func (c *Catalogue) All() []Definition {
	if c == nil {
		return nil
	}
	out := make([]Definition, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.byID[id])
	}
	return out
}

// Lookup returns one definition by id.
func (c *Catalogue) Lookup(id string) (Definition, bool) {
	if c == nil {
		return Definition{}, false
	}
	def, ok := c.byID[id]
	return def, ok
}
