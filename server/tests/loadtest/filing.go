package main

import (
	"context"
	"fmt"
	"sync"
)

// A fleet is filed as it arrives.
//
// Filing a machine under the customer that holds it, and into one of that
// customer's buildings, is what makes it findable the way the product finds it:
// every list a technician opens is narrowed to one of the two. An estate filed
// under nobody is reachable only through the tenant-wide read, which is not a
// read anyone in the field makes.
//
// It has to follow the load rather than precede it. A machine's row is created
// when it registers, so there is nothing to file until then — and the run
// already holds everything else it needs: the plan says which customer this
// machine belongs to, and the credential it dials with carries its identifier.
// So a machine is filed the moment it arrives, one machine at a time, spread
// across the ramp that brought it in rather than gathered into a pass that would
// have to wait for the last arrival.
//
// Filing is not the load. A refusal is counted and the run carries on, because a
// run that died on one refused filing would throw away the measurement it had
// already taken. What the count exists to prevent is the opposite: a run that
// could not file its estate reporting a filed one.

// estateFiledAnnouncement is the line the run prints once the estate it declared
// is filed. Steps that must not start against an unfiled fleet wait for it —
// scripts/loadtest-quic-incluster.sh reads it, the way it already reads the
// fleet announcement.
//
// The browser-side scenarios choose the building they will time in their own
// setup, once, before their first iteration. A scenario started before anything
// is filed therefore reads an empty building for the whole of its run, whatever
// arrives afterwards — so the ordering is a property of the night rather than a
// tidiness.
const estateFiledAnnouncement = "Estate filed"

// estateFiler files each machine once, as it arrives.
type estateFiler struct {
	client      *FixtureClient
	built       BuiltFixture
	credentials agentCredentials
	// estate is how many machines the run draws from, which is what a machine's
	// position is scaled against when its customer is picked.
	estate int
	// readyAt is how many filed machines make the estate worth announcing.
	readyAt int

	mu sync.Mutex
	// filed is keyed by the machine's name, so a machine that comes back after
	// an outage is recognised as one already filed rather than written again.
	filed     map[string]bool
	refused   int
	announced bool
}

// newEstateFiler builds the filer for a run that has a fixture to file against.
func newEstateFiler(client *FixtureClient, built BuiltFixture, credentials agentCredentials,
	estate, readyAt int,
) *estateFiler {
	if readyAt < 1 {
		readyAt = 1
	}
	return &estateFiler{
		client:      client,
		built:       built,
		credentials: credentials,
		estate:      estate,
		readyAt:     readyAt,
		filed:       map[string]bool{},
	}
}

// file files one arrived machine, and reports whether this arrival is the one
// that completes the level the run declared.
//
// It reports the transition rather than the state so the caller announces once:
// a step waiting on that line would otherwise be told the same thing on every
// arrival for the rest of the night.
//
// A run that built no fixture files nothing. That is an ordinary shape — a bare
// run against a local stack brings its own machines and has nobody to file them
// for — so the nil filer is a no-op rather than a refusal.
func (f *estateFiler) file(ctx context.Context, machine tenantAgent) bool {
	if f == nil {
		return false
	}

	f.mu.Lock()
	if f.filed[machine.hostname] {
		f.mu.Unlock()
		return false
	}
	f.mu.Unlock()

	config, err := f.credentials.forAgent(ctx, machine)
	if err != nil {
		return f.refuse(machine, fmt.Errorf("read its credential: %w", err))
	}
	deviceID, ok := deviceIDFrom(config)
	if !ok {
		return f.refuse(machine, fmt.Errorf("its credential carries no identifier"))
	}
	if err := f.client.FileDevice(f.built, machine.agentIndex, f.estate, deviceID); err != nil {
		return f.refuse(machine, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	// Between the check above and here another arrival may have filed this
	// machine, which is a wasted write rather than a wrong one — but counting it
	// twice would move the level.
	if f.filed[machine.hostname] {
		return false
	}
	f.filed[machine.hostname] = true
	if f.announced || len(f.filed) < f.readyAt {
		return false
	}
	f.announced = true
	return true
}

// refuse records one machine the run could not file, and says so once rather
// than once per machine: a fleet whose filing is refused refuses it the same way
// five hundred times, and the first line is the one that names why.
func (f *estateFiler) refuse(machine tenantAgent, err error) bool {
	f.mu.Lock()
	first := f.refused == 0
	f.refused++
	f.mu.Unlock()

	if first {
		fmt.Printf("::warning::could not file %s: %v — the fleet's customer and building are what a scoped read narrows by\n",
			machine.hostname, err)
	}
	return false
}

// counts is how many machines the run filed and how many it could not.
func (f *estateFiler) counts() (filed, refused int) {
	if f == nil {
		return 0, 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.filed), f.refused
}

// arrivalOf composes the two things that happen when a machine arrives: the
// fleet counts it in the phase it happened in, and the estate files it.
//
// They share a moment rather than a purpose. A machine is registered here, which
// is what makes it part of the fleet and what makes its row exist — and the row
// existing is the whole reason filing follows the load rather than preceding it.
// A run with nobody keeping a tally, or nothing to file against, passes nil for
// either half.
func arrivalOf(noteArrival func(), filer *estateFiler, machine tenantAgent) func() {
	return func() {
		if noteArrival != nil {
			noteArrival()
		}
		if filer.file(context.Background(), machine) {
			fmt.Println(estateFiledAnnouncement)
		}
	}
}

// filingLevel is how many filed machines make the estate worth announcing.
//
// The first phase's level, not the whole estate: a profile climbs, so the estate
// is only complete once its tallest phase is reached, and a step waiting for
// that would stand idle through the ramp it was meant to overlap. What the
// browser-side scenarios need is a building that holds machines, and the first
// phase is the first moment the run can promise one.
//
// A run with no profile offers everything at once, so the estate is the level.
// So is a profile whose first phase connects nobody — nought would announce a
// filed estate before anything had arrived.
func filingLevel(profile *Profile, estate int) int {
	if profile == nil || len(profile.Phases) == 0 {
		return estate
	}
	if level := profile.Phases[0].ConnectedAgents; level > 0 {
		return level
	}
	return estate
}
