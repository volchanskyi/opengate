package main

import (
	"errors"
	"time"
)

// latencyObservations lists the series a run publishes, keeping each stage's tail apart
// and carrying the aggregate error rate beside them.
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

	// Refusals are recorded even at zero, which marks the zero as a reading of an actual run.
	observations = append(observations,
		Observation{At: at, Series: "aggregate_rejected", Value: float64(refusedAgents(in.Results))})
	observations = append(observations, targetObservations(at, in.Conservation)...)

	// Registration is reported only when the server was asked; the harness clock stops at a buffer.
	if in.Registration != nil && in.Registration.Measured() {
		observations = append(observations,
			registrationQuantile(at, "register_p95_ms", *in.Registration, 0.95),
			// Under heavy load the tail reflects the queue and the median reflects the write.
			registrationQuantile(at, "register_p50_ms", *in.Registration, 0.50),
			Observation{At: at, Series: "register_mean_ms", Value: in.Registration.MeanMs()},
			Observation{At: at, Series: "register_rejected", Value: float64(in.Registration.Rejected)},
			Observation{At: at, Series: "db_pool_in_use", Value: in.Registration.PoolInUse},
			Observation{At: at, Series: "db_pool_open", Value: in.Registration.PoolOpen},
		)
	}
	return observations
}

// pastTheScale marks a registration figure beyond the widest bucket the server publishes.
const pastTheScale = "past the scale"

// registrationQuantile is one registration quantile as an observation, marked when the server's
// scale ended below it.
func registrationQuantile(at time.Time, series string, reading ServerRegistration, q float64) Observation {
	observation := Observation{At: at, Series: series, Value: reading.QuantileMs(q)}
	if reading.PastTheScale(q) {
		observation.Labels = map[string]string{"reading": pastTheScale}
	}
	return observation
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

// severedMidHold counts the machines whose connection went away while they were held.
// It is recorded even at zero, which is the finding for a fleet that held.
func severedMidHold(results []agentResult) int {
	severed := 0
	for _, result := range results {
		if errors.Is(result.err, ErrHeldPeerGone) {
			severed++
		}
	}
	return severed
}

// targetObservations records what the target held either side of the run, as both readings so
// a later reader can re-divide them. Resident memory is recorded and ungated.
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
