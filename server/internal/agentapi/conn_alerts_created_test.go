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

func alertFor(t *testing.T, ruleID string) *protocol.ControlMessage {
	t.Helper()
	return broken(t, func(msg *protocol.ControlMessage) { msg.RuleID = ruleID })
}

func (f alertFixture) created(t *testing.T, ruleID string) float64 {
	t.Helper()
	return promtestutil.ToFloat64(f.metrics.AlertsCreatedTotal.WithLabelValues(ruleID))
}

// createdReaches waits for the counter to settle; a store outcome lands on the persist-slot
// goroutine.
func (f alertFixture) createdReaches(t *testing.T, ruleID string, want float64) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		return f.created(t, ruleID) == want
	}, 2*time.Second, 5*time.Millisecond, "expected %v alerts counted for %s", want, ruleID)
}

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
			// The wait lets the outcome be accounted for before the counter is read.
			f.dropped(t, tc.reason)
			assert.InDelta(t, tc.want, f.created(t, ruleID), 0)
		})
	}
}

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

func TestCreatedCounterSurvivesAConnectionWithoutMetrics(t *testing.T) {
	t.Parallel()

	f := alertConn(t)
	f.conn.metrics = nil

	assert.NotPanics(t, func() { f.ingest(t, wellFormed(t)) })
	f.reachedStore(t, 1)
}
