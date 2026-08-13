package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rule vocabulary is the one place both sides agree on what a rule may
// watch. It is mirrored by the Rust `RULE_METRICS` / `canonical_rule_metric` in
// mesh-protocol, and the reverse golden go_control_push_alert_rules.bin — which
// this package generates from RuleMetrics and RuleMetricAliases, and the Rust
// harness decodes — is what keeps the two from drifting apart.

func TestCanonicalRuleMetric(t *testing.T) {
	t.Parallel()

	type resolution struct {
		name string
		in   string
		want string
		ok   bool
	}
	cases := []resolution{
		// Rules already pushed to the fleet name the pre-rename dimensions. They
		// must keep watching the same reading, under one name from here on.
		{"legacy memory name", "mem.used", "mem.used_percent", true},
		{"legacy disk name", "disk.used", "disk.used_percent", true},
		{"unknown name", "not.a.metric", "", false},
		{"empty name", "", "", false},
		{"traversal attempt", "../../etc/passwd", "", false},
		// A per-window maximum is a reduction central telemetry publishes, not a
		// reading the evaluator ever holds.
		{"window maximum", "cpu.total.max", "", false},
	}
	for _, name := range RuleMetrics {
		cases = append(cases, resolution{"canonical " + name, name, name, true})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := CanonicalRuleMetric(tc.in)
			assert.Equal(t, tc.ok, ok, "%q resolvable", tc.in)
			assert.Equal(t, tc.want, got, "%q resolves to", tc.in)
		})
	}
}

func TestRuleMetricAliases_TargetTheVocabularyAndAreNotInIt(t *testing.T) {
	t.Parallel()
	canonical := make(map[string]bool, len(RuleMetrics))
	for _, name := range RuleMetrics {
		require.False(t, canonical[name], "%s appears twice in RuleMetrics", name)
		canonical[name] = true
	}
	for alias, target := range RuleMetricAliases {
		assert.True(t, canonical[target], "alias %s points at %s, outside the vocabulary", alias, target)
		assert.False(t, canonical[alias], "%s is both an alias and a canonical name", alias)
	}
}

func TestControlMessageRuleFieldsRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  *ControlMessage
	}{
		{
			name: "a rule using every grammar field",
			msg: &ControlMessage{
				Type: MsgPushAlertRules,
				AlertRules: []ThresholdRule{{
					ID:          "disk-wearing-out",
					Metric:      "disk.await_ms",
					Comparator:  AlertComparatorGte,
					Threshold:   20,
					Clear:       10,
					SustainSecs: 300,
					Predicate:   RulePredicateWindowMean,
					WindowSecs:  600,
					All: []RuleTerm{{
						Metric:     "disk.queue_depth",
						Comparator: AlertComparatorGt,
						Threshold:  8,
						Clear:      4,
						Predicate:  RulePredicateWindowMax,
						WindowSecs: 600,
					}},
				}},
			},
		},
		{
			name: "coverage riding a health summary",
			msg: &ControlMessage{
				Type: MsgAgentHealthSummary,
				TS:   1_700_000_100,
				RuleCoverage: []RuleCoverage{
					{RuleID: "disk-critical", State: RuleCoverageActive},
					{RuleID: "io-stalled", State: RuleCoverageUnsupported},
				},
			},
		},
		{
			// An agent that predates coverage sends no such key. That is
			// "reported nothing", which the server counts as unknown — never a
			// decode failure.
			name: "a summary carrying no coverage at all",
			msg:  &ControlMessage{Type: MsgAgentHealthSummary, TS: 1_700_000_100},
		},
	}

	codec := &Codec{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload, err := codec.EncodeControl(tc.msg)
			require.NoError(t, err)
			got, err := codec.DecodeControl(payload)
			require.NoError(t, err)
			assert.Equal(t, tc.msg.AlertRules, got.AlertRules)
			assert.Equal(t, tc.msg.RuleCoverage, got.RuleCoverage)
		})
	}
}
