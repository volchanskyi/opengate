package main

import (
	"math"
	"slices"
	"strings"
	"time"
)

// fleetOnlineShare is the share of a phase's machines its first figure waits for.
const fleetOnlineShare = 0.95

// fleetOnlineSeries is the series a limit on a phase's fleet-online time names, before the phase.
const fleetOnlineSeries = "quic/online/"

// timedPhases names the phases the profile's limits hold to a fleet-online time.
func timedPhases(profile *Profile) map[string]bool {
	timed := map[string]bool{}
	for _, gate := range profile.Gates {
		if name, ok := strings.CutPrefix(gate.Series, fleetOnlineSeries); ok {
			timed[name] = true
		}
	}
	return timed
}

// markFleetOnline times, for each timed phase that asked for machines, how long after the phase
// began 95% of them and every one had come online, read once the walk ends so stragglers count.
func markFleetOnline(results []PhaseResult, fleet Fleet, timed map[string]bool) {
	for i := range results {
		phase := &results[i]
		offered := phase.askedTo - phase.askedFrom
		if offered <= 0 || !timed[phase.Name] {
			continue
		}
		wave := fleet.Wave(phase.askedFrom, phase.askedTo)
		after := make([]time.Duration, 0, len(wave.OnlineAt))
		for _, at := range wave.OnlineAt {
			after = append(after, max(at.Sub(phase.StartedAt), 0))
		}
		slices.Sort(after)
		phase.FleetOnlineP95Seconds = onlineBy(after, int(math.Ceil(fleetOnlineShare*float64(offered))))
		phase.FleetOnlineAllSeconds = onlineBy(after, offered)
		phase.FleetSignInsPerSecond = signInsWhileWaited(wave)
	}
}

// signInsWhileWaited is the wave's sign-ins per second over the time some machine waited between
// its first dial and the answer, so an idle stretch counts for nothing; absent when none signed in.
func signInsWhileWaited(wave Wave) *float64 {
	type span struct{ from, to time.Time }
	spans := make([]span, 0, len(wave.OnlineAt))
	for i, at := range wave.OnlineAt {
		if i < len(wave.Online) {
			spans = append(spans, span{from: at.Add(-wave.Online[i]), to: at})
		}
	}
	if len(spans) == 0 {
		return nil
	}
	slices.SortFunc(spans, func(a, b span) int { return a.from.Compare(b.from) })

	var waited time.Duration
	current := spans[0]
	for _, next := range spans[1:] {
		if next.from.After(current.to) {
			waited += current.to.Sub(current.from)
			current = next
			continue
		}
		if next.to.After(current.to) {
			current.to = next.to
		}
	}
	waited += current.to.Sub(current.from)
	if waited <= 0 {
		return nil
	}
	rate := float64(len(spans)) / waited.Seconds()
	return &rate
}

// onlineBy is the time the count-th machine came online, absent when fewer than count did.
func onlineBy(sorted []time.Duration, count int) *float64 {
	if count <= 0 || len(sorted) < count {
		return nil
	}
	seconds := sorted[count-1].Seconds()
	return &seconds
}
