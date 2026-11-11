package main

import (
	"fmt"
	"io"
	"strings"
)

// The server's own account of the live readings it received and when its store accepted them.
const (
	liveReceivedMetric   = "opengate_ingest_readings_received_total"
	liveSettledMetric    = "opengate_ingest_readings_total"
	liveAcceptanceBucket = "opengate_ingest_acceptance_seconds_bucket"
	// livePromiseBound is the bucket bound the live promise is read at, in seconds.
	livePromiseBound = "5"
)

// LiveAcceptance is how many live readings the server received during the run, how many its store
// accepted, and how many within the 5 s promise; a reading not stored is a miss.
type LiveAcceptance struct {
	Received          int64 `json:"received"`
	Stored            int64 `json:"stored"`
	WithinFiveSeconds int64 `json:"within_five_seconds"`
}

// Share is the part of the received live readings the store accepted within 5 s.
func (l LiveAcceptance) Share() float64 {
	if l.Received <= 0 {
		return 0
	}
	return float64(l.WithinFiveSeconds) / float64(l.Received)
}

// Since is what the server counted between before and this reading.
func (l LiveAcceptance) Since(before LiveAcceptance) LiveAcceptance {
	return LiveAcceptance{
		Received:          l.Received - before.Received,
		Stored:            l.Stored - before.Stored,
		WithinFiveSeconds: l.WithinFiveSeconds - before.WithinFiveSeconds,
	}
}

// ParseLiveAcceptance reads the live class's counts off the server's metrics page.
func ParseLiveAcceptance(page string) (LiveAcceptance, error) {
	var reading LiveAcceptance
	found := false
	for _, line := range strings.Split(page, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value, ok := splitSample(line)
		if !ok || labels["class"] != "live" {
			continue
		}
		switch {
		case name == liveReceivedMetric:
			reading.Received = int64(value)
			found = true
		case name == liveSettledMetric && labels["outcome"] == "stored":
			reading.Stored = int64(value)
		case name == liveAcceptanceBucket && labels["le"] == livePromiseBound:
			reading.WithinFiveSeconds = int64(value)
		}
	}
	if !found {
		return LiveAcceptance{}, fmt.Errorf("the server's metrics carry no %s for the live class", liveReceivedMetric)
	}
	return reading, nil
}

// readLiveAcceptance reads the server's live counts; no address, or no reading, gives nil.
func readLiveAcceptance(metricsURL string) *LiveAcceptance {
	if metricsURL == "" {
		return nil
	}
	page, err := fetchExpositionPage(metricsURL)
	if err != nil {
		fmt.Printf("::warning::could not read the server's live acceptance: %v\n", err)
		return nil
	}
	reading, err := ParseLiveAcceptance(page)
	if err != nil {
		fmt.Printf("::warning::%v\n", err)
		return nil
	}
	return &reading
}

// liveAcceptanceOver is the run's live acceptance from its two readings; either missing gives nil.
func liveAcceptanceOver(before, after *LiveAcceptance) *LiveAcceptance {
	if before == nil || after == nil {
		return nil
	}
	run := after.Since(*before)
	return &run
}

// writeLiveAcceptance prints the server's account of the run's live readings; nil prints nothing.
func writeLiveAcceptance(w io.Writer, l *LiveAcceptance) {
	if l == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "Live:        %d of %d live readings stored within 5s (%.2f%%), %d stored\n",
		l.WithinFiveSeconds, l.Received, l.Share()*100, l.Stored)
}
