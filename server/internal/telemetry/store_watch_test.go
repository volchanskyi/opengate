package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// scriptedStart answers each read with the next scripted start time, or its error.
type scriptedStart struct {
	starts []time.Time
	errs   []error
	reads  int
}

func (s *scriptedStart) StartTime(context.Context) (time.Time, error) {
	i := s.reads
	s.reads++
	if i < len(s.errs) && s.errs[i] != nil {
		return time.Time{}, s.errs[i]
	}
	return s.starts[i], nil
}

func TestAStoreRestartAsksAgainFromThirtySecondsBeforeTheLastReadOfTheOldStart(t *testing.T) {
	booted := time.Unix(1_763_000_000, 0)
	rebooted := booted.Add(6 * time.Hour)
	clock := time.Unix(1_763_010_000, 0)
	reader := &scriptedStart{
		starts: []time.Time{booted, booted, {}, rebooted, rebooted},
		errs:   []error{nil, nil, errors.New("store down"), nil, nil},
	}
	var asked []time.Time
	watch := NewStoreWatch(reader, func(since time.Time) { asked = append(asked, since) })
	watch.now = func() time.Time { return clock }

	ctx := context.Background()
	watch.Poll(ctx)
	clock = clock.Add(5 * time.Second)
	watch.Poll(ctx)
	lastOldRead := clock
	clock = clock.Add(5 * time.Second)
	watch.Poll(ctx)
	assert.Empty(t, asked, "a store that does not answer has not restarted yet")
	clock = clock.Add(40 * time.Second)
	watch.Poll(ctx)
	clock = clock.Add(5 * time.Second)
	watch.Poll(ctx)

	require.Len(t, asked, 1, "one restart is raised once")
	assert.Equal(t, lastOldRead.Add(-30*time.Second), asked[0])
}

func TestTheFirstStartTimeReadIsNoRestart(t *testing.T) {
	reader := &scriptedStart{starts: []time.Time{time.Unix(1_763_000_000, 0)}}
	raised := false
	watch := NewStoreWatch(reader, func(time.Time) { raised = true })
	watch.Poll(context.Background())
	assert.False(t, raised, "a server that starts after the store has nothing to ask for")
}

func TestVMClientReadsTheStoresStartTime(t *testing.T) {
	before := time.Now()
	client, ctx := NewVMClient(testvm.Dedicated(t), nil), context.Background()
	started, err := client.StartTime(ctx)
	require.NoError(t, err)
	assert.WithinRange(t, started, before.Add(-time.Minute), time.Now().Add(time.Minute),
		"the stamp is the start of the store this test began")
	again, err := client.StartTime(ctx)
	require.NoError(t, err)
	assert.Equal(t, started, again, "a running store keeps its start time")
}

func TestVMClientImportsEncodedReadingsUnderItsEnvironment(t *testing.T) {
	client, ctx := newTestVMClient(t)
	stamped := client.WithNamespace("opengate-staging")
	tenant, device := uuid.New(), uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	readings := encodeReadings(Scope{Tenant: tenant, Device: device}, []Sample{{
		Name: "opengate_test_import_metric", Value: 7, TS: ts, Labels: map[string]string{"dim": "cpu"},
	}})

	require.NoError(t, stamped.Import(ctx, readings.body))
	require.NoError(t, client.Flush(ctx))

	series, err := client.Export(ctx, tenant, `opengate_test_import_metric{device_id="`+device.String()+`"}`,
		ts.Add(-time.Minute), ts.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, series, 1)
	assert.Equal(t, "opengate-staging", series[0].Metric["namespace"])
	assert.Equal(t, []float64{7}, series[0].Values)
}
