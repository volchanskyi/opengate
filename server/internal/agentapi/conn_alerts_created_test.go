package agentapi

import (
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// The aggregate counter behind the platform's own view of a rollout: how many
// alerts each shipped rule is actually producing.
//
// It is the numerator of the alerts-per-device-per-day figure three separate
// decisions currently rest on, so it counts stored rows and only stored rows. A
// replay that changed nothing and a refusal that stored nothing are both not new
// detection, and counting either would inflate the very rate the customer
// ceiling and the evidence projection are sized against.

// alertFor is the well-formed alert attributed to another shipped rule, which is
// how a case says "a different rule fired" without restating the whole message.
func alertFor(t *testing.T, ruleID string) *protocol.ControlMessage {
	t.Helper()
	return broken(t, func(msg *protocol.ControlMessage) { msg.RuleID = ruleID })
}

// created reads how many alerts a rule has been counted for.
func (f alertFixture) created(t *testing.T, ruleID string) float64 {
	t.Helper()
	return promtestutil.ToFloat64(f.metrics.AlertsCreatedTotal.WithLabelValues(ruleID))
}

// createdReaches waits for the counter to settle, because a store outcome lands
// on the persist-slot goroutine rather than the read loop.
func (f alertFixture) createdReaches(t *testing.T, ruleID string, want float64) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		return f.created(t, ruleID) == want
	}, 2*time.Second, 5*time.Millisecond, "expected %v alerts counted for %s", want, ruleID)
}

// TestOnlyAStoredAlertIsCountedAsCreated keeps the rate honest. Q12 divides this
// counter by the fleet, so a replay counted here would report detection that
// never happened, against ceilings sized from the answer.
func TestOnlyAStoredAlertIsCountedAsCreated(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		outcome alerts.Outcome
		reason  string
		want    float64
	}{
		{"a stored alert is new detection", alerts.Stored, "", 1},
		{"a reconnect replay is the alert already held", alerts.Duplicate, alertDropDuplicate, 0},
		{"a refused alert became no row at all", alerts.CeilingSuppressed, alertDropOrganizationCeiling, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := alertConn(t)
			f.store.outcome = tc.outcome
			ruleID, _ := catalogueRule(t)

			f.ingest(t, wellFormed(t))
			if tc.reason == "" {
				f.createdReaches(t, ruleID, tc.want)
				return
			}
			// Wait for the outcome to be accounted for, so the zero below is a
			// counter that stayed still rather than one nothing has reached yet.
			f.dropped(t, tc.reason)
			assert.InDelta(t, tc.want, f.created(t, ruleID), 0)
		})
	}
}

// TestCreatedAlertsAreCountedPerRule is what makes a bad rollout legible: one
// rule's rate climbing while the rest hold steady is a rule that was retuned
// wrong, and a single fleet-wide total would show that as ordinary growth.
func TestCreatedAlertsAreCountedPerRule(t *testing.T) {
	t.Parallel()

	f := alertConn(t)
	fired, _ := catalogueRule(t)

	f.ingest(t, wellFormed(t))
	f.ingest(t, alertFor(t, "io-stalled"))
	f.ingest(t, alertFor(t, "io-stalled"))

	f.createdReaches(t, fired, 1)
	f.createdReaches(t, "io-stalled", 2)
	assert.InDelta(t, 0, f.created(t, "memory-pressure"), 0,
		"a rule that has not fired reads zero, not missing")
}

// TestAnUnshippedRuleMintsNoCounterLabel is the cardinality bound. A rule id
// arrives from the endpoint, and the alert path already refuses one this build
// has no definition for — this pins that the refusal happens before the label
// does, which is what keeps the series count at the size of the rule pack.
func TestAnUnshippedRuleMintsNoCounterLabel(t *testing.T) {
	t.Parallel()

	f := alertConn(t)

	f.ingest(t, alertFor(t, "rule-from-a-newer-agent"))
	f.dropped(t, alertDropRuleUnknown)

	assert.InDelta(t, 0, f.created(t, "rule-from-a-newer-agent"), 0,
		"an alert refused for naming an unknown rule never reaches the counter")
	assert.InDelta(t, 0, f.created(t, appmetrics.UnknownRule), 0,
		"and it is not folded into the catch-all either — it was never stored")
}

// TestCreatedCounterSurvivesAConnectionWithoutMetrics keeps a wiring detail from
// being able to take down an ingest path.
func TestCreatedCounterSurvivesAConnectionWithoutMetrics(t *testing.T) {
	t.Parallel()

	f := alertConn(t)
	f.conn.metrics = nil

	assert.NotPanics(t, func() { f.ingest(t, wellFormed(t)) })
	f.reachedStore(t, 1)
}
