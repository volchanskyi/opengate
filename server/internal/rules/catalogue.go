// Package rules owns what a monitoring rule may say and which machines it is
// watching.
//
// A rule has three layers, and they are separated by how mutable they are:
//
//   - Its definition — the predicate, the grammar it is written in, the evidence
//     an alert carries, and what its alerts are grouped by — is versioned YAML
//     compiled into the server. Definitions are immutable per (id, version), so
//     the rule that raised an alert last week still means what it meant then.
//   - A customer's parameter overrides live in Postgres, keyed down the tenancy
//     ladder, because a threshold is exactly the thing an operator retunes.
//   - A rule's rollout state — whether it is on, how far it has reached, and the
//     kill switch — lives in Postgres too, because stopping a rule cannot
//     require a deploy.
//
// Keeping definitions out of the database is what makes the program's highest
// leverage gate possible: a predicate is cost-bounded in CI, before it reaches
// an endpoint, where the analysis is free and a bad rule costs nothing. The same
// check in a runtime API path would be a production incident instead.
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
	// maxRuleIDLen bounds a rule id. Ids travel to the agent and come back on
	// breaches and coverage reports, so they stay short and plain.
	maxRuleIDLen = 64
)

// groupByVocabulary is what a rule's alerts may be grouped by. Grouping is what
// turns repeated firings into one thing an operator looks at, so the key has to
// name something the server can actually group on.
var groupByVocabulary = map[string]bool{
	"device":       true,
	"site":         true,
	"organization": true,
	"mount":        true,
	"metric":       true,
}

// evidenceVocabulary is what an alert raised by a rule may carry with it. Each
// entry names a body of evidence the server already collects; a rule cannot ask
// for something nobody gathers.
var evidenceVocabulary = map[string]bool{
	"vitals":        true,
	"top_processes": true,
	"recent_logs":   true,
	"inventory":     true,
	"correlation":   true,
}

// comparatorVocabulary maps the catalogue's spelling of a comparison to the wire
// enum the agent decodes.
var comparatorVocabulary = map[string]protocol.AlertComparator{
	"gt":  protocol.AlertComparatorGt,
	"lt":  protocol.AlertComparatorLt,
	"gte": protocol.AlertComparatorGte,
	"lte": protocol.AlertComparatorLte,
}

// predicateVocabulary is how a rule may derive the number it compares. An empty
// name is the plain instant threshold a rule states by saying nothing.
var predicateVocabulary = map[string]protocol.RulePredicate{
	"":           protocol.RulePredicateInstant,
	"Instant":    protocol.RulePredicateInstant,
	"Rate":       protocol.RulePredicateRate,
	"WindowMax":  protocol.RulePredicateWindowMax,
	"WindowMean": protocol.RulePredicateWindowMean,
}

const (
	// minRuleVersion is the first revision a definition may declare. Counting
	// from one leaves nothing for an absent revision to be mistaken for.
	minRuleVersion = 1
	// maxRuleVersion is the last one the wire can carry, which is what bounds
	// it: an alert names the revision that fired, so a revision that did not
	// survive the journey would identify an alert as something else.
	maxRuleVersion = uint64(math.MaxUint32)
)

// KindEvent names a rule that watches the machine's own log records. A rule
// that names no kind watches a reading, which is what every rule in the file
// was before this one existed.
const KindEvent = "event"

// severityVocabulary maps the file's spelling of how bad a rule is to the enum
// an alert carries. The set is closed at the database too, so a severity
// outside it would be a write that fails rather than a screen that renders
// nothing.
var severityVocabulary = map[string]protocol.AlertSeverity{
	"info":     protocol.AlertSeverityInfo,
	"warning":  protocol.AlertSeverityWarning,
	"critical": protocol.AlertSeverityCritical,
}

// Bounds is the range a tunable parameter may be set to. A binding outside it is
// refused on write, so the rule's author decides how far an operator can go.
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

// Definition is one rule as the catalogue states it: immutable per
// (ID, Version), and the whole of what the rule means.
type Definition struct {
	ID      string `yaml:"id" json:"id"`
	Version int    `yaml:"version" json:"version"`
	// Kind is what the rule watches. Empty is a rule about a reading, which is
	// what every rule was when the file held only those; KindEvent is a rule
	// about the words the machine writes about itself.
	//
	// The two are evaluated by different machinery on the endpoint and only one
	// of them travels: a rule about a reading is sent to the machine and
	// compared there, while the phrases a rule about words matches on are what
	// the machine's log reader is built around and stay compiled into it. What
	// this file carries for one of those is the rest of the rule — its name,
	// its revision, how bad it is, and where its alerts belong — which is
	// exactly what the server needs to accept an alert, place it in a room and
	// let an administrator stop it.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	// Severity is how bad this rule's alerts are. It orders the queue: a rule
	// that stated none would file a disk about to stop accepting writes beside
	// one that merely feels slow.
	Severity string `yaml:"severity" json:"severity"`
	// Summary says what the rule is for, in an operator's words. It is
	// documentation rather than behavior, and is deliberately outside the
	// immutability digest so a clearer wording is not a version bump.
	Summary string `yaml:"summary" json:"-"`

	Metric         string  `yaml:"metric" json:"metric"`
	ComparatorName string  `yaml:"comparator" json:"comparator"`
	Threshold      float64 `yaml:"threshold" json:"threshold"`
	Clear          float64 `yaml:"clear" json:"clear"`
	SustainSecs    uint32  `yaml:"sustain_secs" json:"sustain_secs"`
	PredicateName  string  `yaml:"predicate" json:"predicate"`
	WindowSecs     uint32  `yaml:"window_secs" json:"window_secs"`
	All            []Term  `yaml:"all" json:"all"`

	// GroupBy is what this rule's alerts are about — the key repeated firings
	// collapse onto. A rule without one cannot be correlated or de-duplicated.
	GroupBy []string `yaml:"group_by" json:"group_by"`
	// GroupWindowSecs is how long firings on one group key stay one alert.
	GroupWindowSecs uint32 `yaml:"group_window_secs" json:"group_window_secs"`
	// Evidence is what an alert from this rule carries with it.
	Evidence []string `yaml:"evidence" json:"evidence"`
	// CoverageRequires names the metrics a device must be able to read for this
	// rule to be evaluable on it. A device that cannot read one of them reports
	// the rule unsupported rather than quietly never firing.
	CoverageRequires []string `yaml:"coverage_requires" json:"coverage_requires"`
	// Tunable declares which parameters a customer binding may override, and how
	// far. A parameter absent here cannot be bound at all.
	Tunable map[string]Bounds `yaml:"tunable" json:"tunable"`
}

// WatchesEvents reports whether this rule reads the machine's own words rather
// than one of its readings.
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

// Digest is the fingerprint of what this definition means. Prose is excluded
// (see Summary), so the digest changes exactly when behavior does.
func (d Definition) Digest() (string, error) {
	encoded, err := json.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("digest rule %s: %w", d.ID, err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// ShippedParam returns the definition's own value for a tunable parameter, and
// whether name is one. The names it answers for are the fields a customer
// binding may override: the numbers on the rule, never its shape — retuning a
// threshold is configuration, while changing the metric or the predicate is a
// different rule.
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

// catalogueFile is the YAML shape of one pack file.
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
