package api

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// gridWindow is the fixture window shared by the grid tests: seven days from a
// step-aligned instant, bucketed at 600 s → 1008 buckets.
var (
	gridFrom = time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	gridTo   = gridFrom.Add(7 * 24 * time.Hour)
	gridStep = 600 * time.Second
)

func nonNullSlots(vals []*float64) []int {
	var slots []int
	for i, v := range vals {
		if v != nil {
			slots = append(slots, i)
		}
	}
	return slots
}

// TestBuildMetricGridComesFromTheRequest pins the axis to (from, to, step) only.
// A window's bucket count is span/step whatever the store happens to hold, so a
// 7 d request can never render as the two points a near-empty device returns.
func TestBuildMetricGridComesFromTheRequest(t *testing.T) {
	t.Parallel()
	g := buildMetricGrid(gridFrom, gridTo, gridStep)

	require.Len(t, g.ts, 1008, "span/step buckets, independent of any data")
	assert.Equal(t, gridFrom.Unix(), g.ts[0])
	for i, ts := range g.ts {
		require.Equal(t, g.ts[0]+int64(i)*600, ts, "bucket %d is off the lattice", i)
	}
	// The end instant is exclusive: the last bucket sits one step short of `to`.
	assert.Equal(t, gridTo.Unix()-600, g.ts[1007])
	assert.Equal(t, int64(600), g.step)
}

// TestBuildMetricGridFloorsToTheStepLattice pins the alignment rule VM itself
// applies: a start that is not a whole multiple of the step is rounded *down*.
// Matching it is what keeps every value in its own bucket instead of shifted by
// one — see the measured evidence in the testvm alignment test.
func TestBuildMetricGridFloorsToTheStepLattice(t *testing.T) {
	t.Parallel()
	unaligned := gridFrom.Add(7 * time.Second)
	g := buildMetricGrid(unaligned, unaligned.Add(time.Hour), 60*time.Second)

	assert.Equal(t, gridFrom.Unix(), g.ts[0], "start floors onto the step lattice")
	assert.Zero(t, g.ts[0]%60)
	assert.Len(t, g.ts, 60)
}

// TestBuildMetricGridAlwaysHasOneBucket keeps a sub-step window renderable: a
// 5 s window at the 10 s raw cadence is one bucket, never an empty axis that
// would make the response shapeless.
func TestBuildMetricGridAlwaysHasOneBucket(t *testing.T) {
	t.Parallel()
	g := buildMetricGrid(gridFrom, gridFrom.Add(5*time.Second), minRangeStepSecs*time.Second)
	require.Len(t, g.ts, 1)
	assert.Equal(t, gridFrom.Unix(), g.queryStart().Unix())
	assert.Equal(t, gridFrom.Unix(), g.queryEnd().Unix())
}

// TestBuildMetricGridQueryBoundsAreTheGridEdges proves the query and the axis
// cannot drift: the range read is issued at exactly the first and last bucket,
// both whole multiples of the step, so VictoriaMetrics evaluates the query on
// the grid the response declares.
func TestBuildMetricGridQueryBoundsAreTheGridEdges(t *testing.T) {
	t.Parallel()
	g := buildMetricGrid(gridFrom.Add(31*time.Second), gridTo, gridStep)

	assert.Equal(t, g.ts[0], g.queryStart().Unix())
	assert.Equal(t, g.ts[len(g.ts)-1], g.queryEnd().Unix())
	assert.Zero(t, g.queryStart().Unix()%600)
	assert.Zero(t, g.queryEnd().Unix()%600)
}

// TestBuildMetricGridStaysWithinMaxPoints pins that the request-derived builder
// cannot widen the response beyond the bound chooseStep already enforces —
// wide windows get coarser buckets, never more of them.
func TestBuildMetricGridStaysWithinMaxPoints(t *testing.T) {
	t.Parallel()
	for _, window := range []time.Duration{
		time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 365 * 24 * time.Hour,
	} {
		for _, maxPoints := range []int{minMaxPointsBound, defaultMaxPoints, maxMaxPointsBound} {
			to := gridFrom.Add(window)
			g := buildMetricGrid(gridFrom, to, chooseStep(gridFrom, to, maxPoints))
			assert.LessOrEqual(t, len(g.ts), maxPoints, "window %s at max_points %d", window, maxPoints)
			assert.LessOrEqual(t, len(g.ts), maxMaxPointsBound)
			assert.NotEmpty(t, g.ts)
		}
	}
}

// TestMetricGridSlotRejectsOffLatticePoints pins the projection rule: a
// timestamp only claims a bucket when it is on the lattice and inside the
// window. Everything else is a defect the caller must account for, not a value
// nudged into a neighbouring slot.
func TestMetricGridSlotRejectsOffLatticePoints(t *testing.T) {
	t.Parallel()
	g := buildMetricGrid(gridFrom, gridFrom.Add(time.Hour), 600*time.Second)
	first := g.ts[0]

	for _, tc := range []struct {
		name string
		ts   int64
		want int
		ok   bool
	}{
		{"first bucket", first, 0, true},
		{"last bucket", first + 5*600, 5, true},
		{"one second past a bucket", first + 601, 0, false},
		{"one second before a bucket", first + 599, 0, false},
		{"before the window", first - 600, 0, false},
		{"past the window", first + 6*600, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slot, ok := g.slot(tc.ts)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.Equal(t, tc.want, slot)
			}
		})
	}
}

// TestAssembleMetricRangeProjectsSparseAnswerOntoFullGrid is the A3 defect in
// miniature: VictoriaMetrics answers 2 of 1008 buckets and the response still
// spans the full requested window, with an honest hole everywhere else.
func TestAssembleMetricRangeProjectsSparseAnswerOntoFullGrid(t *testing.T) {
	t.Parallel()
	g := buildMetricGrid(gridFrom, gridTo, gridStep)
	avg := []telemetry.RangeSeries{{
		Labels:     map[string]string{"dim": "cpu.util"},
		Timestamps: []int64{g.ts[500], g.ts[501]},
		Values:     []float64{12, 13},
	}}

	got, off := assembleMetricRange(avg, nil, nil, nil, false, g)

	assert.Zero(t, off.count)
	require.Len(t, got.T, 1008, "the axis is the requested window, not the answer")
	assert.Equal(t, g.ts, got.T)
	assert.Equal(t, 600, got.BucketS)
	assert.True(t, got.Downsampled)
	require.Len(t, got.Series, 1)

	vals := got.Series[0].Avg
	require.Len(t, vals, 1008)
	assert.Equal(t, []int{500, 501}, nonNullSlots(vals), "only the answered buckets carry a value")
	assert.InDelta(t, 12.0, *vals[500], 1e-9)
	assert.InDelta(t, 13.0, *vals[501], 1e-9)
}

// TestAssembleMetricRangeCountsOffGridPoints applies the WS-A accounting lesson
// to the read path: a point that does not land on the grid is a defect, so it is
// counted (and reported for the log) rather than dropped in silence.
func TestAssembleMetricRangeCountsOffGridPoints(t *testing.T) {
	t.Parallel()
	g := buildMetricGrid(gridFrom, gridFrom.Add(time.Hour), 600*time.Second)
	avg := []telemetry.RangeSeries{{
		Labels: map[string]string{"dim": "cpu.util"},
		Timestamps: []int64{
			g.ts[0] + 1,             // shifted off the lattice
			g.ts[0] - 600,           // before the window
			g.ts[len(g.ts)-1] + 600, // past the window
			g.ts[2],                 // the one honest point
		},
		Values: []float64{1, 2, 3, 4},
	}}

	got, off := assembleMetricRange(avg, nil, nil, nil, false, g)

	assert.Equal(t, 3, off.count)
	assert.Equal(t, "cpu.util", off.dim, "the log names the offending dimension")
	assert.Equal(t, g.ts[0]+1, off.ts, "the log names the first offending timestamp")
	assert.Equal(t, []int{2}, nonNullSlots(got.Series[0].Avg), "an off-grid point never lands in a neighbouring bucket")
}

// TestAssembleMetricRangeBandSharesTheRequestGrid keeps min/max on the same axis
// as the avg line. A band aligned to a different grid would draw a fill that
// does not belong to the line it wraps.
func TestAssembleMetricRangeBandSharesTheRequestGrid(t *testing.T) {
	t.Parallel()
	g := buildMetricGrid(gridFrom, gridTo, gridStep)
	dim := map[string]string{"dim": "cpu.util"}
	avg := []telemetry.RangeSeries{{Labels: dim, Timestamps: []int64{g.ts[7]}, Values: []float64{20}}}
	mins := []telemetry.RangeSeries{{Labels: dim, Timestamps: []int64{g.ts[7]}, Values: []float64{10}}}
	maxs := []telemetry.RangeSeries{{Labels: dim, Timestamps: []int64{g.ts[7], g.ts[7] + 1}, Values: []float64{30, 99}}}

	got, off := assembleMetricRange(avg, mins, maxs, nil, true, g)

	require.Len(t, got.Series, 1)
	assert.Equal(t, MetricSeriesMinMaxSourceAvgOf10s, got.Series[0].MinMaxSource)
	require.NotNil(t, got.Series[0].Min)
	require.NotNil(t, got.Series[0].Max)
	assert.Len(t, *got.Series[0].Min, 1008)
	assert.Len(t, *got.Series[0].Max, 1008)
	assert.Equal(t, []int{7}, nonNullSlots(*got.Series[0].Min))
	assert.Equal(t, []int{7}, nonNullSlots(*got.Series[0].Max))
	assert.Equal(t, 1, off.count, "the band's off-grid point is accounted for too")
}

// TestAssembleMetricRangeSkipsNonFiniteValues keeps a NaN/Inf out of the payload:
// the slot stays null (a gap) rather than serialising an unrepresentable number.
func TestAssembleMetricRangeSkipsNonFiniteValues(t *testing.T) {
	t.Parallel()
	g := buildMetricGrid(gridFrom, gridFrom.Add(time.Hour), 600*time.Second)
	avg := []telemetry.RangeSeries{{
		Labels:     map[string]string{"dim": "cpu.util"},
		Timestamps: []int64{g.ts[0], g.ts[1], g.ts[2]},
		Values:     []float64{math.NaN(), math.Inf(1), 5},
	}}

	got, off := assembleMetricRange(avg, nil, nil, nil, false, g)

	assert.Zero(t, off.count, "a non-finite value is not a grid defect")
	assert.Equal(t, []int{2}, nonNullSlots(got.Series[0].Avg))
}

// TestAssembleMetricRangeToleratesTruncatedSeries stops a malformed answer from
// panicking the read path: values are read only where a timestamp has one.
func TestAssembleMetricRangeToleratesTruncatedSeries(t *testing.T) {
	t.Parallel()
	g := buildMetricGrid(gridFrom, gridFrom.Add(time.Hour), 600*time.Second)
	avg := []telemetry.RangeSeries{{
		Labels:     map[string]string{"dim": "cpu.util"},
		Timestamps: []int64{g.ts[0], g.ts[1], g.ts[2]},
		Values:     []float64{1},
	}}

	got, off := assembleMetricRange(avg, nil, nil, nil, false, g)

	assert.Zero(t, off.count)
	assert.Equal(t, []int{0}, nonNullSlots(got.Series[0].Avg))
}
