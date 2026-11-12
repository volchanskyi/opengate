package telemetry_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

func TestNothingIsWrittenForAMachineAfterItsDenyListEntry(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	var erased sync.Map
	cfg := quickConfig(store, nil)
	cfg.Tombstoned = func(device uuid.UUID) bool { _, gone := erased.Load(device); return gone }
	w := started(t, cfg)
	got := newAnswers()
	machine := anyScope()

	erased.Store(machine.Device, struct{}{})
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: machine,
		Samples: readings(3, readingTime), Done: got.done(1)})

	assert.ErrorIs(t, got.await(t, 2*time.Second).Err, telemetry.ErrTombstoned)
	assert.NotContains(t, store.written(), machine.Device.String(), "an erased machine's readings never reach the store")
}

func TestErasureWaitsForAWriteWhoseCheckPassedBeforeTheEntry(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	inFlight := make(chan struct{})
	store := &fakeStore{hold: func([]byte) {
		close(inFlight)
		<-release
	}}
	w := started(t, quickConfig(store, nil))
	got := newAnswers()
	machine := anyScope()
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: machine,
		Samples: readings(3, readingTime), Done: got.done(1)})
	<-inFlight

	waited := make(chan error, 1)
	go func() { waited <- w.WaitForWrites(context.Background(), machine.Tenant, &machine.Device) }()
	select {
	case <-waited:
		t.Fatal("series deletion went ahead while a write registered before the entry was in flight")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-waited)
	got.await(t, time.Second)
}

func TestErasureOfOneMachineDoesNotWaitForAnother(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	defer close(release)
	inFlight := make(chan struct{})
	store := &fakeStore{hold: func([]byte) {
		select {
		case <-inFlight:
		default:
			close(inFlight)
		}
		<-release
	}}
	w := started(t, quickConfig(store, nil))
	busy := anyScope()
	w.Submit(telemetry.Submission{Class: telemetry.ClassLive, Scope: busy,
		Samples: readings(1, readingTime), Done: func(telemetry.Outcome) {}})
	<-inFlight

	other := uuid.New()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, w.WaitForWrites(ctx, busy.Tenant, &other))
	require.ErrorIs(t, w.WaitForWrites(ctx, busy.Tenant, nil), context.DeadlineExceeded,
		"a tenant's erasure waits for every machine of the tenant")
}
