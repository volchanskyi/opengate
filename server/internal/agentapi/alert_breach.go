package agentapi

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

const maxAlertRuleIDLen = 64

// alertBreachSamples turns firing WS-19 breaches into VM samples, dropping any
// whose metric is outside the rule vocabulary and sanitizing the rule id label
// (agent-echoed, so defense-in-depth against control chars and overlong values).
//
// The metric label is the canonical name the reported one resolves to, so an
// agent that predates the vitals rename and one that follows it write the same
// series rather than two halves of one story. The vocabulary doubles as the
// bound on that label: a breach naming anything else cannot drive central
// cardinality because it is not recorded at all.
func alertBreachSamples(breaches []protocol.AlertBreach, ts time.Time) []telemetry.Sample {
	if len(breaches) == 0 {
		return nil
	}
	samples := make([]telemetry.Sample, 0, len(breaches))
	for _, breach := range breaches {
		metric, ok := protocol.CanonicalRuleMetric(breach.Metric)
		if !ok {
			continue
		}
		ruleID := sanitizeAlertRuleID(breach.RuleID)
		if ruleID == "" {
			continue
		}
		samples = append(samples, telemetry.Sample{
			Name:   "opengate_edge_alert_breach",
			Value:  breach.Value,
			TS:     ts,
			Labels: map[string]string{"rule": ruleID, "metric": metric},
		})
	}
	return samples
}

// sanitizeAlertRuleID trims, rune-caps, and control-char-redacts an agent-echoed
// rule id before it becomes a metric label.
func sanitizeAlertRuleID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.ContainsAny(id, " \t\r\n") {
		return "[redacted]"
	}
	if utf8.RuneCountInString(id) > maxAlertRuleIDLen {
		id = string([]rune(id)[:maxAlertRuleIDLen])
	}
	return id
}
