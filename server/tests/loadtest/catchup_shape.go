package main

import (
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// gapBatches is the catch-up a machine owes after losing gap: one reading of each vital for every
// closed minute of the gap, in batches of at most maxPerBatch, each ending at its newest minute.
func gapBatches(gap time.Duration, now time.Time, maxPerBatch int) [][]protocol.BackfillSample {
	closed := now.Truncate(time.Minute)
	minutes := int(gap / time.Minute)
	if minutes <= 0 || maxPerBatch <= 0 {
		return nil
	}
	var batches [][]protocol.BackfillSample
	batch := make([]protocol.BackfillSample, 0, maxPerBatch)
	for m := minutes; m >= 1; m-- {
		ts := closed.Add(-time.Duration(m) * time.Minute).Unix()
		for i, name := range defaultMetricDimNames {
			if len(batch) == maxPerBatch {
				batches = append(batches, batch)
				batch = make([]protocol.BackfillSample, 0, maxPerBatch)
			}
			batch = append(batch, protocol.BackfillSample{Name: name, TS: ts, Value: float64(i)})
		}
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

// agentBatchLimit is the most readings a shipped agent puts in one catch-up batch.
const agentBatchLimit = 1000

// plannedBatches is what one machine sends after its grant: the gap it lost when the run names
// one, else the counted batches it was asked for.
func plannedBatches(opts loadOptions, now time.Time) [][]protocol.BackfillSample {
	if opts.backfillGap > 0 {
		return gapBatches(opts.backfillGap, now, agentBatchLimit)
	}
	pending := max(opts.backfillBatches*opts.backfillSamplesPerBatch, 0)
	startTS := now.Add(-time.Duration(pending) * time.Second).Unix()
	batches := make([][]protocol.BackfillSample, opts.backfillBatches)
	for i := range batches {
		batches[i] = buildBackfillSamples(opts.backfillSamplesPerBatch, startTS+int64(i))
	}
	return batches
}
