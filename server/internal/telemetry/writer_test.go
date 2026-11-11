package telemetry_test

import (
	"errors"
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

func TestEveryReadingCarriesTheConnectionsTenantAndDevice(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	w := started(t, quickConfig(store, nil))
	scope := anyScope()
	got := newAnswers()

	samples := readings(2, readingTime)
	samples = append(samples, telemetry.Sample{
		Name: "opengate_edge_metric_avg", Value: 1, TS: readingTime,
		Labels: map[string]string{"dim": "cpu.total", "device_id": uuid.NewString()},
	})
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: scope, Samples: samples, Done: got.done(1)})

	outcome := got.await(t, 2*time.Second)
	require.NoError(t, outcome.Err)
	assert.Equal(t, 2, outcome.Stored)
	assert.Equal(t, 1, outcome.Refused, "a reading naming its own device is refused, not obeyed")
	written := store.written()
	assert.Equal(t, 2, strings.Count(written, `device_id="`+scope.Device.String()+`"`))
	assert.Equal(t, 2, strings.Count(written, `tenant_id="`+scope.Tenant.String()+`"`))
}

func TestOneBadReadingFailsNothingElse(t *testing.T) {
	t.Parallel()
	_, w, got := plainWriter(t)

	samples := readings(3, readingTime)
	samples[1].Value = math.NaN()
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: anyScope(),
		Samples: samples, Done: got.done(1)})

	outcome := got.await(t, 2*time.Second)
	require.NoError(t, outcome.Err)
	assert.Equal(t, telemetry.Outcome{Stored: 2, Refused: 1}, outcome)
}

func TestReadingsReceivedEqualStoredPlusRefusedByReason(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	obs := newRecorder()
	cfg := quickConfig(store, obs)
	cfg.LiveBound = 4096
	tombstoned := uuid.New()
	cfg.Tombstoned = func(device uuid.UUID) bool { return device == tombstoned }
	w := started(t, cfg)
	got := newAnswers()
	at := readingTime

	bad := readings(4, at)
	bad[0].Value = math.Inf(1)
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: anyScope(),
		Samples: bad, Done: got.done(1)})
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: telemetry.Scope{Tenant: uuid.New(), Device: tombstoned},
		Samples: readings(2, at), Done: got.done(2)})
	for range 2 {
		got.await(t, 2*time.Second)
	}
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: anyScope(),
		Samples: readings(200, at), Done: got.done(3)})
	assert.ErrorIs(t, got.await(t, time.Second).Err, telemetry.ErrQueueFull)

	store.failWith(errors.New("store unavailable"))
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: anyScope(),
		Samples: readings(5, at), Done: got.done(4)})
	assert.ErrorIs(t, got.await(t, 2*time.Second).Err, telemetry.ErrStoreWrite)

	got.answeredOnce(t, 4)
	settled := obs.settled("live")
	assert.Equal(t, map[string]int{
		"stored": 3, "invalid": 1, "tombstoned": 2, "queue_full": 200, "write_failed": 5,
	}, settled)
	total := 0
	for _, n := range settled {
		total += n
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	assert.Equal(t, obs.received["live"], total, "every reading received is stored or refused under a reason")
}

func TestQueuedAndInFlightBytesNeverExceedTheBoundOfTheirClass(t *testing.T) {
	t.Parallel()
	store := &fakeStore{hold: func([]byte) { time.Sleep(2 * time.Millisecond) }}
	obs := newRecorder()
	cfg := quickConfig(store, obs)
	cfg.LiveBound = 64 * 1024
	cfg.CatchUpBound = 128 * 1024
	cfg.FlushBytes = 16 * 1024
	w := started(t, cfg)
	got := newAnswers()

	var wg sync.WaitGroup
	var next atomic.Int64
	for range 64 {
		wg.Go(func() {
			for range 20 {
				id := int(next.Add(1))
				class := telemetry.ClassLive
				if id%2 == 0 {
					class = telemetry.ClassCatchUp
				}
				w.Submit(telemetry.Submission{Class: class,
					Scope:   anyScope(),
					Samples: readings(16, readingTime), Done: got.done(id)})
			}
		})
	}
	wg.Wait()
	for range 64 * 20 {
		got.await(t, 5*time.Second)
	}

	got.answeredOnce(t, 64*20)
	obs.mu.Lock()
	defer obs.mu.Unlock()
	assert.LessOrEqual(t, obs.peak["live"], cfg.LiveBound, "live never holds more than its bound")
	assert.LessOrEqual(t, obs.peak["catch_up"], cfg.CatchUpBound, "catch-up never holds more than its bound")
	assert.Positive(t, obs.peak["live"])
}

func TestTheWriterRunsTheSameGoroutinesWhateverTheFleetSize(t *testing.T) {
	_, w, got := plainWriter(t)
	baseline := runtime.NumGoroutine()

	sent := 0
	for _, machines := range []int{10, 100, 1000} {
		for range machines {
			sent++
			w.Submit(telemetry.Submission{Class: telemetry.ClassLive,
				Scope:   anyScope(),
				Samples: readings(1, readingTime), Done: got.done(sent)})
		}
		for range machines {
			got.await(t, 5*time.Second)
		}
		assert.LessOrEqualf(t, runtime.NumGoroutine(), baseline+1,
			"%d machines writing add no goroutines to the writer", machines)
	}
}

func TestAFullRequestGoesWithoutWaitingForItsAge(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	cfg := quickConfig(store, nil)
	cfg.FlushAge = time.Hour
	cfg.FlushBytes = 1024
	w := started(t, cfg)
	got := newAnswers()

	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: anyScope(),
		Samples: readings(40, readingTime), Done: got.done(1)})

	require.NoError(t, got.await(t, 2*time.Second).Err, "a request at its size goes at once")
}

func TestAnEmptySubmissionIsAnsweredStoredAtOnce(t *testing.T) {
	t.Parallel()
	store, w, got := plainWriter(t)

	w.Submit(telemetry.Submission{Class: telemetry.ClassCatchUp, Scope: anyScope(),
		Done: got.done(1)})

	assert.Equal(t, telemetry.Outcome{}, got.await(t, time.Second))
	assert.Empty(t, store.written())
}

func TestReadingsEncodedBeforeTheHandOffAreQueuedAsTheyAre(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	obs := newRecorder()
	w := started(t, quickConfig(store, obs))
	got := newAnswers()
	scope := anyScope()
	samples := readings(3, readingTime)
	samples[2].Value = math.NaN()
	encoded := telemetry.Encode(scope, samples)
	require.Positive(t, encoded.Size())

	w.Submit(telemetry.Submission{Class: telemetry.ClassCatchUp, Scope: scope, Encoded: encoded, Done: got.done(1)})

	assert.Equal(t, telemetry.Outcome{Stored: 2, Refused: 1}, got.await(t, 2*time.Second))
	assert.Len(t, store.written(), encoded.Size(), "what was weighed is what is written")
	obs.mu.Lock()
	defer obs.mu.Unlock()
	assert.Equal(t, 3, obs.received["catch_up"])
}
