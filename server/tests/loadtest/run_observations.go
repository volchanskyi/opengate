package main

import (
	"errors"
	"time"
)

// What a run publishes about itself, as series a limit or a trend can read.
//
// It is separated from the assembly of the bundle around it because the two
// answer different questions: that file decides what a finished run *was*, and
// this one decides which of it is worth a number somebody downstream compares
// against last night's.

// latencyObservations records each phase's tail separately. Folding them into
// one aggregate hides which of the three a slow run was slow in, and they are
// three different pieces of work.
//
// The aggregate error rate travels beside them because it is the third of the
// three series the profiles hold their machine-side limits to, and it was the
// one a bundle did not carry — so on the venues whose only output is a bundle,
// every limit named a measurement nothing there could produce.
func latencyObservations(at time.Time, connect, handshake []time.Duration, errorRate float64,
	in runBundleInputs,
) []Observation {
	observations := []Observation{
		{At: at, Series: "connect_p95_ms", Value: millis(percentile(connect, 95))},
		{At: at, Series: "handshake_p95_ms", Value: millis(percentile(handshake, 95))},
		{At: at, Series: "aggregate_error_rate", Value: errorRate},
	}

	observations = append(observations,
		Observation{At: at, Series: "agents_severed_mid_hold", Value: float64(severedMidHold(in.Results))})

	// What a declared ceiling turned away. It reached one field of one phase and
	// travelled no further, so a run refused at a limit and a run that was not
	// looked identical in everything a night prints — and now that a refusal is
	// out of the error rate, the two are the same zero as well.
	//
	// Recorded even when it is nought, because nought is the finding: it is what
	// says the zero beside it is a reading rather than an empty denominator.
	observations = append(observations,
		Observation{At: at, Series: "aggregate_rejected", Value: float64(refusedAgents(in.Results))})
	observations = append(observations, targetObservations(at, in.Conservation)...)

	// Registration is reported only when the server was asked. Its own clock
	// stops at a local send buffer, and a number that cannot move is worse than
	// an absent one: two ceilings sat on it for months.
	if in.Registration != nil && in.Registration.Measured() {
		observations = append(observations,
			Observation{At: at, Series: "register_p95_ms", Value: in.Registration.QuantileMs(0.95)},
			// The middle case beside the tail. They answer different questions
			// about the same queue, and where the venue is driven to what it
			// has been shown to hold only one of them reproduces: two runs an
			// hour apart under identical load read tails of 5,773 and 9,443 ms
			// with middle cases of 239 and 255. The tail there is the queue;
			// the middle case is the write.
			Observation{At: at, Series: "register_p50_ms", Value: in.Registration.QuantileMs(0.50)},
			Observation{At: at, Series: "register_mean_ms", Value: in.Registration.MeanMs()},
			Observation{At: at, Series: "register_rejected", Value: float64(in.Registration.Rejected)},
			Observation{At: at, Series: "db_pool_in_use", Value: in.Registration.PoolInUse},
			Observation{At: at, Series: "db_pool_open", Value: in.Registration.PoolOpen},
		)
	}
	return observations
}

// refusedAgents counts the machines a declared ceiling turned away.
func refusedAgents(results []agentResult) int {
	refused := 0
	for _, result := range results {
		if errors.Is(result.err, ErrEnrollmentRefused) {
			refused++
		}
	}
	return refused
}

// severedMidHold counts the machines whose connection went away while they were
// being held.
//
// It is recorded even when it is zero, because zero is the finding: a run that
// held a hundred machines for eight minutes and severed none of them says so,
// and the same run reporting a hundred successes while its fleet was gone is
// what this number exists to make impossible.
func severedMidHold(results []agentResult) int {
	severed := 0
	for _, result := range results {
		if errors.Is(result.err, ErrHeldPeerGone) {
			severed++
		}
	}
	return severed
}

// targetObservations records what the target was holding either side of the
// run, so the bundle carries the question as well as the verdict.
//
// Both readings travel rather than the difference alone: a bundle is read years
// after the metrics store forgot the night, and a delta cannot be re-divided by
// a denominator a later reader wants to change. Open file descriptors travel
// with them because they are what separates a goroutine leak from a socket
// leak — their flatness through a 344 MiB climb is what ruled sockets out.
//
// Resident memory is recorded and not gated, for the reason
// maxRetainedGoroutinesPerOperation states.
func targetObservations(at time.Time, target TargetConservation) []Observation {
	if !target.Start.Read && !target.End.Read {
		return nil
	}
	return []Observation{
		{At: at, Series: "target_goroutines_start", Value: target.Start.Goroutines},
		{At: at, Series: "target_goroutines_end", Value: target.End.Goroutines},
		{At: at, Series: "target_resident_bytes_start", Value: target.Start.ResidentBytes},
		{At: at, Series: "target_resident_bytes_end", Value: target.End.ResidentBytes},
		{At: at, Series: "target_open_fds_start", Value: target.Start.OpenFDs},
		{At: at, Series: "target_open_fds_end", Value: target.End.OpenFDs},
		{At: at, Series: "target_completed_operations", Value: float64(target.Operations)},
		{At: at, Series: "target_retained_goroutines_per_operation", Value: target.RetainedGoroutinesPerOperation()},
		{At: at, Series: "target_retained_bytes_per_operation", Value: target.RetainedBytesPerOperation()},
	}
}
