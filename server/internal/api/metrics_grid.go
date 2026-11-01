package api

import "time"

// metricGrid is the time axis of a range response, derived from the request alone.
// Its edges sit on the step lattice because VictoriaMetrics rounds an unaligned start down to it.
type metricGrid struct {
	ts   []int64
	step int64
}

// buildMetricGrid lays out span/step buckets with an exclusive end; a shorter window gets one.
func buildMetricGrid(from, to time.Time, step time.Duration) metricGrid {
	stepSecs := int64(step.Seconds())
	if stepSecs < 1 {
		stepSecs = 1
	}
	buckets := int64(to.Sub(from).Seconds()) / stepSecs
	if buckets < 1 {
		buckets = 1
	}

	// The start floors onto the step lattice, as VictoriaMetrics rounds a range query's start.
	rem := from.Unix() % stepSecs
	if rem < 0 {
		rem += stepSecs
	}
	first := from.Unix() - rem

	ts := make([]int64, buckets)
	for i := range ts {
		ts[i] = first + int64(i)*stepSecs
	}
	return metricGrid{ts: ts, step: stepSecs}
}

// queryStart and queryEnd are the grid's first and last bucket, where the range read is issued.
func (g metricGrid) queryStart() time.Time { return time.Unix(g.ts[0], 0).UTC() }
func (g metricGrid) queryEnd() time.Time   { return time.Unix(g.ts[len(g.ts)-1], 0).UTC() }

// slot locates a timestamp's bucket; a timestamp off the lattice or outside the window has none.
func (g metricGrid) slot(ts int64) (int, bool) {
	offset := ts - g.ts[0]
	if offset < 0 || offset%g.step != 0 {
		return 0, false
	}
	i := offset / g.step
	if i >= int64(len(g.ts)) {
		return 0, false
	}
	return int(i), true
}

func clampMaxPoints(mp *int) int {
	v := defaultMaxPoints
	if mp != nil {
		v = *mp
	}
	if v < minMaxPointsBound {
		return minMaxPointsBound
	}
	if v > maxMaxPointsBound {
		return maxMaxPointsBound
	}
	return v
}

// chooseStep picks the smallest whole-second bucket, at least 60 s, that fits maxPoints.
func chooseStep(from, to time.Time, maxPoints int) time.Duration {
	windowSecs := int64(to.Sub(from).Seconds())
	if windowSecs <= 0 {
		return minRangeStepSecs * time.Second
	}
	step := (windowSecs + int64(maxPoints) - 1) / int64(maxPoints)
	if step < minRangeStepSecs {
		step = minRangeStepSecs
	}
	return time.Duration(step) * time.Second
}
