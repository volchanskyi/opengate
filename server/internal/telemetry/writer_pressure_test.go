package telemetry_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

func TestALiveReadingIsAcceptedWhileACatchUpWriteIsHeld(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	defer close(release)
	catchUpHeld := make(chan struct{})
	store := &fakeStore{hold: func(body []byte) {
		if bytes.Contains(body, []byte("catchup-host")) {
			close(catchUpHeld)
			<-release
		}
	}}
	w := started(t, quickConfig(store, nil))
	got := newAnswers()

	catchUp := readings(10, readingTime)
	for i := range catchUp {
		catchUp[i].Labels = map[string]string{"dim": "catchup-host"}
	}
	w.Submit(telemetry.Submission{Class: telemetry.ClassCatchUp, Scope: anyScope(),
		Samples: catchUp, Done: func(telemetry.Outcome) {}})
	<-catchUpHeld

	handed := time.Now()
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: anyScope(),
		Samples: readings(3, time.Unix(1_763_000_060, 0)), Done: got.done(1)})
	outcome := got.await(t, 5*time.Second)
	require.NoError(t, outcome.Err)
	assert.Less(t, time.Since(handed), 5*time.Second, "a live reading never waits behind a catch-up write")
}

func TestLiveMinutesDroppedAreReportedOnceTheQueueDrains(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	held := make(chan struct{}, 1)
	store := &fakeStore{hold: func([]byte) {
		select {
		case held <- struct{}{}:
			<-release
		default:
		}
	}}
	cfg := quickConfig(store, nil)
	cfg.LiveBound = 4096
	w := started(t, cfg)
	got := newAnswers()
	busy := anyScope()
	dropped := anyScope()
	minute := time.Unix(1_763_000_040, 0).UTC()

	// About half the bound is in flight, and a hand-off of thirty readings is larger than the rest.
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: busy,
		Samples: readings(12, minute), Done: got.done(1)})
	<-held
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: dropped,
		Samples: readings(30, minute.Add(time.Minute)), Done: got.done(2)})
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: dropped,
		Samples: readings(30, minute), Done: got.done(3)})
	for range 2 {
		require.ErrorIs(t, got.await(t, time.Second).Err, telemetry.ErrQueueFull)
	}

	assert.Empty(t, w.LiveDrops(), "nothing is asked for while the live queue is above its low mark")
	close(release)
	require.NoError(t, got.await(t, 2*time.Second).Err)

	var drops map[uuid.UUID]time.Time
	require.Eventually(t, func() bool { drops = w.LiveDrops(); return len(drops) > 0 },
		2*time.Second, 10*time.Millisecond)
	assert.Equal(t, map[uuid.UUID]time.Time{dropped.Device: minute}, drops,
		"each machine is asked again from its first dropped minute")
	assert.Empty(t, w.LiveDrops(), "a drop is reported once")
}

func TestACatchUpBatchWaitingPastItsBoundIsAnsweredWaitedTooLong(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	held := make(chan struct{}, 1)
	store := &fakeStore{hold: func([]byte) {
		select {
		case held <- struct{}{}:
			<-release
		default:
		}
	}}
	cfg := quickConfig(store, nil)
	cfg.QueueWait = 50 * time.Millisecond
	w := started(t, cfg)
	got := newAnswers()

	w.Submit(telemetry.Submission{Class: telemetry.ClassCatchUp, Scope: anyScope(),
		Samples: readings(1, readingTime), Done: got.done(1)})
	<-held
	w.Submit(telemetry.Submission{Class: telemetry.ClassCatchUp, Scope: anyScope(),
		Samples: readings(1, readingTime), Done: got.done(2)})
	time.Sleep(100 * time.Millisecond)
	close(release)

	first, second := got.await(t, 2*time.Second), got.await(t, 2*time.Second)
	require.NoError(t, first.Err)
	assert.ErrorIs(t, second.Err, telemetry.ErrWaitedTooLong, "a batch kept past its bound is answered, not written")
}

func TestShutdownAnswersEverythingStillQueued(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	held := make(chan struct{}, 1)
	store := &fakeStore{hold: func([]byte) {
		select {
		case held <- struct{}{}:
			<-release
		default:
		}
	}}
	cfg := quickConfig(store, nil)
	cfg.ShutdownGrace = 50 * time.Millisecond
	w := telemetry.NewWriter(cfg)
	ctx, stop := context.WithCancel(context.Background())
	done := w.Start(ctx)
	got := newAnswers()

	w.Submit(telemetry.Submission{Class: telemetry.ClassCatchUp, Scope: anyScope(),
		Samples: readings(1, readingTime), Done: got.done(1)})
	<-held
	w.Submit(telemetry.Submission{Class: telemetry.ClassCatchUp, Scope: anyScope(),
		Samples: readings(1, readingTime), Done: got.done(2)})
	stop()
	close(release)
	<-done

	outcomes := []telemetry.Outcome{got.await(t, time.Second), got.await(t, time.Second)}
	got.answeredOnce(t, 2)
	var shutdown int
	for _, o := range outcomes {
		if errors.Is(o.Err, telemetry.ErrShuttingDown) {
			shutdown++
		}
	}
	assert.LessOrEqual(t, shutdown, 1, "what was in flight is answered by its request")

	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: anyScope(),
		Samples: readings(1, readingTime), Done: got.done(3)})
	assert.ErrorIs(t, got.await(t, time.Second).Err, telemetry.ErrShuttingDown,
		"a hand-off after shutdown is answered at once")
}
