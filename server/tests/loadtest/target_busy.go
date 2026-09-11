package main

import (
	"fmt"
	"strings"
	"time"
)

// How hard the target worked, per phase.
//
// A phase reported the wait times it saw and the machines that turned up, and
// from those two alone a server that has run out of processor and a server that
// is idle but slow are the same picture. They are different problems with
// different fixes — one is answered by giving the target more, the other by
// changing what it does — so every statement of the form "the server was not
// working hard" was an inference until this reading existed.
//
// It is read off the page the harness already fetches registration timing and
// goroutine counts from, which the client library's process collector publishes
// on every target this repository runs. A counter of processor seconds means
// nothing on its own, so it is bracketed around the phase and divided by two
// things: the phase's own wall clock, and the processor allowance whoever
// started the target declared. What comes out is *the target used N% of what it
// was given*, which is comparable between a rung with half a processor and a
// rung with four.
//
// A reading that could not be taken is absent, never nought. Nought is a target
// that did no work at all, and a reader that fills an unanswered question in
// with it hands every comparison the one answer that always looks healthy.

// targetCPUMetric is the family the target publishes its own processor time in.
const targetCPUMetric = "process_cpu_seconds_total"

// Why a reading is absent, in the phase's own words.
//
// An absent reading voids a bundle, on the reasoning that a run which read the
// target once could have read it again — so a phase that did not is a reading
// somebody dropped. There is a case that reasoning does not cover, and it is
// the one the breakpoint family exists to reach: a target loaded until it stops
// answering. That is the finding rather than a lapse, and a phase that says so
// keeps its place in the evidence while a phase that says nothing still does
// not.
//
// The other two are narrower and are stated for the same reason: an absence a
// reader cannot account for is one they have to guess at.
const (
	busyAbsentTargetSilent    = "the target did not answer its own exposition"
	busyAbsentNoAllowance     = "nobody declared what the target was capped at"
	busyAbsentTargetRestarted = "the target restarted inside the phase"
	busyAbsentNoWindow        = "the phase had no length to divide the work by"
)

// TargetBusy is how a phase reads what share of its allowance the target used.
//
// Both halves are needed and either can be missing. A run pointed at no target
// has nothing to read; a run that was never told what the target is capped at
// has processor seconds and no denominator, and dividing by a guess is how a
// figure stops being a reading.
type TargetBusy struct {
	// ReadCPUSeconds is the target's cumulative processor time, and whether it
	// could be read at all.
	ReadCPUSeconds func() (float64, bool)
	// Allowance is the processor share the target is capped at — the same
	// figure the target's fingerprint carries, since a bundle whose busy-ness
	// and whose fingerprint disagreed about the denominator would be worse than
	// one carrying neither.
	Allowance float64
}

// Bracket takes the opening reading and returns what closes it.
//
// The closer is handed the window it covers, which is the phase's own clock
// rather than the phase's declaration: a phase runs for as long as it runs, and
// dividing a real amount of work by a declared length reports a busy-ness
// nobody measured.
// The closer returns the reading and, where there is none, what accounted for
// it. A run pointed at no target asked nothing, so it gets neither: there is no
// absence to account for where there was no question.
func (b TargetBusy) Bracket() func(window time.Duration) (*float64, string) {
	if b.ReadCPUSeconds == nil {
		return func(time.Duration) (*float64, string) { return nil, "" }
	}

	before, opened := b.read()

	return func(window time.Duration) (*float64, string) {
		after, closed := b.read()
		if !opened || !closed {
			return nil, busyAbsentTargetSilent
		}
		if b.Allowance <= 0 {
			return nil, busyAbsentNoAllowance
		}
		if window <= 0 {
			return nil, busyAbsentNoWindow
		}

		// A counter that went backwards is a target that restarted inside the
		// phase. The two readings then describe two processes, and their
		// difference is a negative amount of work — so the phase says it
		// measured nothing rather than reporting one.
		used := after - before
		if used < 0 {
			return nil, busyAbsentTargetRestarted
		}

		percent := used / window.Seconds() / b.Allowance * 100
		return &percent, ""
	}
}

func (b TargetBusy) read() (float64, bool) {
	if b.ReadCPUSeconds == nil {
		return 0, false
	}
	return b.ReadCPUSeconds()
}

// NewTargetBusy is the reading a run takes against a live target. A run given
// no address to read reads nothing, which every phase then reports as an
// absence.
func NewTargetBusy(metricsURL string, allowance float64) TargetBusy {
	if metricsURL == "" {
		return TargetBusy{}
	}
	return TargetBusy{
		ReadCPUSeconds: func() (float64, bool) {
			return FetchTargetCPUSeconds(metricsURL)
		},
		Allowance: allowance,
	}
}

// FetchTargetCPUSeconds reads the processor time the target has used since it
// started.
func FetchTargetCPUSeconds(baseURL string) (float64, bool) {
	page, err := fetchExpositionPage(baseURL)
	if err != nil {
		fmt.Printf("::warning::could not read how hard the target was working: %v\n", err)
		return 0, false
	}
	return ParseTargetCPUSeconds(page)
}

// ParseTargetCPUSeconds reads the processor counter out of an exposition page.
//
// It reads only that family, the way the two readers beside it do, so what the
// harness depends on stays visible in one place rather than behind a parser for
// the whole format.
func ParseTargetCPUSeconds(page string) (float64, bool) {
	for _, line := range strings.Split(page, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, value, ok := splitSample(line)
		if ok && name == targetCPUMetric {
			return value, true
		}
	}
	return 0, false
}
