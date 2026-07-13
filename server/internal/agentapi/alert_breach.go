package agentapi

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// alertBreachMetrics bounds the `metric` label of a WS-19 breach sample to the
// sampler dimensions a rule can watch, so an agent-supplied breach cannot drive
// unbounded label cardinality.
var alertBreachMetrics = map[string]struct{}{
	"cpu.total": {},
	"mem.used":  {},
	"disk.used": {},
}

const maxAlertRuleIDLen = 64

// alertBreachSamples turns firing WS-19 breaches into VM samples, dropping any
// whose metric is outside the known vocabulary and sanitizing the rule id label
// (agent-echoed, so defense-in-depth against control chars and overlong values).
func alertBreachSamples(breaches []protocol.AlertBreach, ts time.Time) []telemetry.Sample {
	if len(breaches) == 0 {
		return nil
	}
	samples := make([]telemetry.Sample, 0, len(breaches))
	for _, breach := range breaches {
		if _, ok := alertBreachMetrics[breach.Metric]; !ok {
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
			Labels: map[string]string{"rule": ruleID, "metric": breach.Metric},
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
