package alerts

import "time"

const (
	// noiseWindow is what "lately" means: the count on the badge.
	noiseWindow = time.Hour
	// noiseHistory is the span the rule's usual rate is worked out over.
	noiseHistory = 7 * 24 * time.Hour

	// quietestBaseline floors the compared rate so a rarely firing rule is not red on one firing.
	quietestBaseline = 1.0

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
	// NoiseHigh is a rule doing several times what it usually does.
	NoiseHigh NoiseLevel = "high"
)

// Noise is one rule's recent count and the rate it is being judged against.
type Noise struct {
	RuleID string
	// Recent is how many alerts the rule raised for this customer in the last hour.
	Recent int
	// BaselinePerHour is the rule's own usual hourly rate for this customer.
	BaselinePerHour float64
	// HasHistory is whether there is any past to compare against at all.
	HasHistory bool
}

// Level is the colour the badge takes, judged against the rule's own history.
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
