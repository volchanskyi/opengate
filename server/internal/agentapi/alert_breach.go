package agentapi

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

const maxAlertRuleIDLen = 64

// alertBreachSamples turns firing breaches into samples labelled by sanitized rule id and
// canonical metric, and skips any metric outside the rule vocabulary to bound cardinality.
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
