package telemetry

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discardStore takes every request whole and keeps nothing of it.
type discardStore struct{ bytes int }

func (s *discardStore) Import(_ context.Context, parts ...[]byte) error {
	for _, p := range parts {
		s.bytes += len(p)
	}
	return nil
}

// bytesAllocatedBy is what fn allocated on the heap.
func bytesAllocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// hourOfMinutes is one machine's hour of one dimension, a catch-up batch's shape.
func hourOfMinutes() []Sample {
	start := time.Unix(1_800_000_000, 0).UTC()
	samples := make([]Sample, 60)
	for i := range samples {
		samples[i] = Sample{Name: "opengate_edge_metric_avg", Value: float64(i) / 3,
			TS: start.Add(time.Duration(i) * time.Minute), Labels: map[string]string{"dim": "cpu.total"}}
	}
	return samples
}

func idleWriter(store Importer) *Writer {
	cfg := DefaultWriterConfig()
	cfg.Store = store
	cfg.QueueWait = time.Hour
	return NewWriter(cfg)
}

func TestTheWriterSendsWhatItQueuedWithoutCopyingIt(t *testing.T) {
	store := &discardStore{}
	w := idleWriter(store)
	q := w.queues[ClassCatchUp]
	scope := Scope{Tenant: uuid.New(), Device: uuid.New()}
	for range 100 {
		w.Submit(Submission{Class: ClassCatchUp, Scope: scope, Samples: hourOfMinutes(), Done: func(Outcome) {}})
	}
	queued, _ := q.due(time.Now())
	require.Greater(t, queued, 512<<10, "a request of over half a mebibyte")

	allocated := bytesAllocatedBy(func() { w.sendOnce(q) })

	assert.Equal(t, queued, store.bytes, "every queued byte reached the store")
	assert.Less(t, allocated, uint64(queued/16), "the request reads the queued bodies where they lie")
}

func TestABodyTheStoreTookIsReusedForTheNextReadings(t *testing.T) {
	w := idleWriter(&discardStore{})
	q := w.queues[ClassCatchUp]
	scope := Scope{Tenant: uuid.New(), Device: uuid.New()}
	samples := hourOfMinutes()
	body := len(Encode(scope, samples).readings.body)
	cycle := func() {
		w.Submit(Submission{Class: ClassCatchUp, Scope: scope, Samples: samples, Done: func(Outcome) {}})
		w.sendOnce(q)
	}
	cycle()

	const cycles = 50
	allocated := bytesAllocatedBy(func() {
		for range cycles {
			cycle()
		}
	}) / cycles

	assert.Less(t, allocated, uint64(body/4), "a %d-byte body comes back from the last request", body)
}

func TestABodyAnsweredUnsentIsReusedToo(t *testing.T) {
	cfg := DefaultWriterConfig()
	cfg.Store = &discardStore{}
	cfg.Tombstoned = func(uuid.UUID) bool { return true }
	w := NewWriter(cfg)
	q := w.queues[ClassCatchUp]
	scope := Scope{Tenant: uuid.New(), Device: uuid.New()}
	samples := hourOfMinutes()
	body := len(Encode(scope, samples).readings.body)
	cycle := func() {
		w.Submit(Submission{Class: ClassCatchUp, Scope: scope, Samples: samples, Done: func(Outcome) {}})
		w.sendOnce(q)
	}
	cycle()

	const cycles = 50
	allocated := bytesAllocatedBy(func() {
		for range cycles {
			cycle()
		}
	}) / cycles

	assert.Less(t, allocated, uint64(body/4), "an erased machine's body is reused unsent")
}
