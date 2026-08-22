package agentapi

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// What reaches the store, and what the server does with each answer it gets
// back. Admission is settled in conn_alerts_test.go; this is the half after it.

// recordingAlertStore captures what the ingest path offered the store, and
// answers with whatever outcome a case needs.
type recordingAlertStore struct {
	mu       sync.Mutex
	got      []alerts.Alert
	grouping []alerts.Grouping
	outcome  alerts.Outcome
	err      error
}

func (s *recordingAlertStore) Record(_ context.Context, a alerts.Alert, g alerts.Grouping) (alerts.Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	s.got = append(s.got, a)
	s.grouping = append(s.grouping, g)
	if s.outcome == "" {
		return alerts.Stored, nil
	}
	return s.outcome, nil
}

// recorded returns what the store was offered once the persist slot has run.
func (s *recordingAlertStore) recorded() []alerts.Alert {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]alerts.Alert(nil), s.got...)
}

// groupedBy returns the grouping the ingest path resolved for each alert.
func (s *recordingAlertStore) groupedBy() []alerts.Grouping {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]alerts.Grouping(nil), s.grouping...)
}

// fail makes every later Record report the store as unreachable.
func (s *recordingAlertStore) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// TestAdmittedAlertReachesTheStoreWhole proves the ingest path hands on
// everything the store needs to file the alert, rather than a subset that would
// leave the row unable to answer for the event.
func TestAdmittedAlertReachesTheStoreWhole(t *testing.T) {
	t.Parallel()
	f := alertConn(t)

	msg := wellFormed(t)
	f.ingest(t, msg)
	got := f.reachedStore(t, 1)[0]

	assert.Equal(t, f.scope.OrganizationID, got.OrganizationID,
		"the customer a machine belongs to is what every scoping key is built on")
	assert.Equal(t, f.scope.DeviceID, got.DeviceID)
	assert.Equal(t, msg.RuleID, got.RuleID)
	assert.Equal(t, msg.RuleVersion, got.RuleVersion)
	assert.Equal(t, alerts.SeverityCritical, got.Severity,
		"the wire's spelling is mapped to the one the store keeps")
	assert.Equal(t, "disk.used_percent", got.Metric)
	require.NotNil(t, got.Value)
	assert.InEpsilon(t, 98.2, *got.Value, 0.0001)
	assert.Equal(t, msg.WindowStartTS, got.WindowStart.Unix())
	assert.Equal(t, msg.WindowEndTS, got.WindowEnd.Unix())
	assert.Equal(t, msg.ObservedTS, got.ObservedAt.Unix())
	assert.False(t, got.Backfilled)
	assert.Equal(t, protocol.EvidenceCodec, got.EvidenceCodec)
	assert.Equal(t, msg.Evidence, got.Evidence, "evidence is stored exactly as it arrived")
	assert.Zero(t, f.conn.DroppedTelemetryCount())
}

// TestStoreOutcomesAreEachCountedAsWhatTheyAre keeps the three answers legible.
// A spent budget cost the customer an incident nobody can reconstruct; a replay
// cost nothing, because the alert is already held. Filing both as one number
// would hide the first behind the second.
func TestStoreOutcomesAreEachCountedAsWhatTheyAre(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		outcome       alerts.Outcome
		reason        string
		alsoSuppresed bool
	}{
		{
			name:          "a spent budget is a lost alert and is counted as suppression",
			outcome:       alerts.CeilingSuppressed,
			reason:        alertDropOrganizationCeiling,
			alsoSuppresed: true,
		},
		{
			name:    "a reconnect replay balances the ledger without being called suppression",
			outcome: alerts.Duplicate,
			reason:  alertDropDuplicate,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := alertConn(t)
			f.store.outcome = tc.outcome

			f.ingest(t, wellFormed(t))
			f.dropped(t, tc.reason)

			// The drop and the suppression are two counters written one after
			// the other on the persist-slot goroutine, and `dropped` waits on
			// the first of them. Reading the second immediately is a race the
			// test loses under load, so each case waits for its own answer:
			// suppression must arrive, or must never arrive.
			suppressed := func() float64 {
				return promtestutil.ToFloat64(
					f.metrics.AlertsSuppressedTotal.WithLabelValues(string(alerts.CeilingSuppressed)))
			}
			if tc.alsoSuppresed {
				require.Eventuallyf(t, func() bool { return suppressed() == 1 },
					2*time.Second, 5*time.Millisecond,
					"a refused alert must be counted as suppression")
				return
			}
			assert.Never(t, func() bool { return suppressed() != 0 },
				200*time.Millisecond, 10*time.Millisecond,
				"only a refused alert is suppression")
		})
	}
}

// TestAlertIsNotAcknowledgedWhenTheStoreIsDown drives E19. The edge retries on
// the next reconnect and that retry is only safe because nothing here reports an
// alert held when it is not: a store failure swallowed here would lose the alert
// permanently, since the endpoint drops it once it believes it landed.
func TestAlertIsNotAcknowledgedWhenTheStoreIsDown(t *testing.T) {
	t.Parallel()
	f := alertConn(t)
	f.store.fail(errors.New("postgres unavailable"))

	// The connection survives: an unreachable store is not a reason to tear down
	// the channel that also carries this device's remote-management paths.
	f.ingest(t, wellFormed(t))
	f.dropped(t, "persist_failed")
	assert.Empty(t, f.store.recorded())

	// The same alert, replayed after the store comes back, lands. The identity
	// on the wire is what makes that replay resolve to one row.
	f.store.fail(nil)
	f.ingest(t, wellFormed(t))
	f.reachedStore(t, 1)
}

// TestAlertWithNoCustomerIsRefusedRatherThanGuessed covers the one thing the
// store cannot be given a default for. Every scoping key an incident is built on
// is the customer's, so an alert filed under a guess would land in another
// customer's room.
func TestAlertWithNoCustomerIsRefusedRatherThanGuessed(t *testing.T) {
	t.Parallel()
	f := alertConn(t)
	f.conn.settings = fixedReader{scope: settings.Scope{
		DeviceID: f.scope.DeviceID, TenantID: f.scope.TenantID,
	}}

	f.ingest(t, wellFormed(t))
	f.dropped(t, alertDropOrganizationUnknown)
	assert.Empty(t, f.store.recorded())
}

// TestBackfilledAlertMayBeMonthsOld pins the one place the clock bound widens. A
// retroactive scan over local history is the whole point of asking "has this
// happened before?", and every answer it produces is older than the live window.
func TestBackfilledAlertMayBeMonthsOld(t *testing.T) {
	t.Parallel()
	f := alertConn(t)
	old := time.Now().UTC().Add(-30 * 24 * time.Hour)
	backfilled := true

	msg := broken(t, stamped(old))
	msg.Backfilled = &backfilled
	f.ingest(t, msg)

	got := f.reachedStore(t, 1)[0]
	assert.True(t, got.Backfilled)
	assert.Equal(t, old.Unix(), got.WindowStart.Unix(),
		"a retroactive finding folds by when it happened, not when it arrived")

	// The retroactive bound is not unbounded: older than the agent's own store
	// could hold is a broken sender, not a finding.
	tooOld := broken(t, stamped(time.Now().UTC().Add(-2*365*24*time.Hour)))
	tooOld.Backfilled = &backfilled
	f.ingest(t, tooOld)
	f.dropped(t, alertDropTimestampOutOfRange)
}

// TestAlertDoesNotBlockTheReadLoop covers the trap this path is most likely to
// fall into. Alerts arrive on the same goroutine as every other control message,
// so a synchronous store write would stall the channel that also carries this
// device's remote-management paths — and a storm is exactly when that channel
// matters most.
func TestAlertDoesNotBlockTheReadLoop(t *testing.T) {
	t.Parallel()
	f := alertConn(t)
	f.conn.telemetrySlots = make(chan struct{}, telemetryConcurrentWrites)
	for range telemetryConcurrentWrites {
		f.conn.telemetrySlots <- struct{}{}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		f.ingest(t, wellFormed(t))
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the read loop blocked on a store write")
	}

	f.dropped(t, "persist_slots_full")
}

// TestAlertWithNoStoreWiredIsCountedRatherThanLost keeps a deployment mistake
// visible. A server wired without a store cannot hold an alert, and reporting
// that as an ordinary admission would make an unmonitored fleet look healthy.
func TestAlertWithNoStoreWiredIsCountedRatherThanLost(t *testing.T) {
	t.Parallel()
	f := alertConn(t)
	f.conn.alertStore = nil

	f.ingest(t, wellFormed(t))
	f.dropped(t, "persist_failed")
}
