package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheWritersReadingsAreCountedByClassAndOutcome(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	m.ReadingsReceived("live", 10)
	m.ReadingsSettled("live", "stored", 8)
	m.ReadingsSettled("live", "queue_full", 2)
	m.ReadingsAccepted("live", 1500*time.Millisecond, 8)
	m.Held("catch_up", 4096, 3*time.Second)
	m.Request("catch_up", 2048, 200*time.Millisecond, nil)
	m.Request("catch_up", 1024, 5*time.Second, errors.New("store down"))

	assert.InDelta(t, 10, testutil.ToFloat64(m.IngestReadingsReceivedTotal.WithLabelValues("live")), 0)
	assert.InDelta(t, 8, testutil.ToFloat64(m.IngestReadingsTotal.WithLabelValues("live", "stored")), 0)
	assert.InDelta(t, 2, testutil.ToFloat64(m.IngestReadingsTotal.WithLabelValues("live", "queue_full")), 0)
	assert.InDelta(t, 4096, testutil.ToFloat64(m.IngestQueueBytes.WithLabelValues("catch_up")), 0)
	assert.InDelta(t, 3, testutil.ToFloat64(m.IngestQueueOldestSeconds.WithLabelValues("catch_up")), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.IngestRequestsTotal.WithLabelValues("catch_up", "failed")), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.IngestRequestsTotal.WithLabelValues("catch_up", "ok")), 0)
	require.Equal(t, 1, testutil.CollectAndCount(m.IngestAcceptanceSeconds), "one class has accepted readings")
}

func TestEveryIngestOutcomeStartsAtZero(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	for _, class := range IngestClasses() {
		for _, outcome := range IngestOutcomes() {
			assert.InDelta(t, 0, testutil.ToFloat64(m.IngestReadingsTotal.WithLabelValues(class, outcome)), 0,
				"%s %s", class, outcome)
		}
		assert.InDelta(t, 0, testutil.ToFloat64(m.IngestReadingsReceivedTotal.WithLabelValues(class)), 0)
	}
	assert.Equal(t, len(IngestClasses())*len(IngestOutcomes()), testutil.CollectAndCount(m.IngestReadingsTotal))
	for _, cause := range ResendCauses() {
		assert.InDelta(t, 0, testutil.ToFloat64(m.IngestResendRequestsTotal.WithLabelValues(cause)), 0, cause)
	}
}

func TestResendRequestsAndStoreRestartsAreCounted(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	m.ObserveStoreRestart()
	m.ObserveResendRequests(ResendStoreRestart, 3)
	m.ObserveResendRequests(ResendLiveDrop, 1)

	assert.InDelta(t, 1, testutil.ToFloat64(m.IngestStoreRestartsTotal), 0)
	assert.InDelta(t, 3, testutil.ToFloat64(m.IngestResendRequestsTotal.WithLabelValues(ResendStoreRestart)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.IngestResendRequestsTotal.WithLabelValues(ResendLiveDrop)), 0)
}
