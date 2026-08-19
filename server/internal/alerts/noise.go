package alerts

import "time"

// How noisy a rule has been lately, and whether that is unusual for it.
//
// The number on its own answers nothing. A rule meant to fire forty times a day
// firing forty times a day is the system working; the same forty on a rule that
// normally fires twice is what somebody has to look at. So the badge is a
// comparison against the rule's own recent history rather than against a
// threshold shared across a pack, and a rule with no history yet reads neutral
// instead of alarming — a fresh customer whose whole pack showed red would be
// told nothing at all.

const (
	// noiseWindow is what "lately" means: the count on the badge.
	noiseWindow = time.Hour
	// noiseHistory is what the rule's usual rate is worked out over. Long enough
	// that a single bad afternoon does not become the rule's normal, short
	// enough that a rule retuned last week is judged against how it behaves now.
	noiseHistory = 7 * 24 * time.Hour

	// quietestBaseline floors the rate a rule is compared against. Without it a
	// rule that fires once a fortnight goes red the moment it fires once, which
	// is the rule doing its job.
	quietestBaseline = 1.0

	// Where the colours change, as multiples of the rule's usual rate.
	elevatedRatio = 1.5
	highRatio     = 3.0
)

// NoiseLevel is the colour on the badge.
type NoiseLevel string

const (
	// NoiseUnknown is a rule with no history to be judged against.
	NoiseUnknown NoiseLevel = "unknown"
	// NoiseQuiet is a rule that has raised nothing lately.
	NoiseQuiet NoiseLevel = "quiet"
	// NoiseUsual is a rule doing roughly what it always does.
	NoiseUsual NoiseLevel = "usual"
	// NoiseElevated is a rule doing noticeably more than it usually does.
	NoiseElevated NoiseLevel = "elevated"
	// NoiseHigh is a rule doing several times what it usually does, which is
	// what a retune is for.
	NoiseHigh NoiseLevel = "high"
)

// Noise is one rule's recent count and the rate it is being judged against.
type Noise struct {
	RuleID string
	// Recent is how many alerts the rule raised for this customer in the last
	// hour.
	Recent int
	// BaselinePerHour is the rule's own usual hourly rate for this customer.
	BaselinePerHour float64
	// HasHistory is whether there is any past to compare against at all.
	HasHistory bool
}

// Level is the colour the badge takes.
func (n Noise) Level() NoiseLevel {
	switch {
	case !n.HasHistory:
		return NoiseUnknown
	case n.Recent == 0:
		return NoiseQuiet
	}

	usual := max(n.BaselinePerHour, quietestBaseline)
	switch ratio := float64(n.Recent) / usual; {
	case ratio > highRatio:
		return NoiseHigh
	case ratio > elevatedRatio:
		return NoiseElevated
	default:
		return NoiseUsual
	}
}
