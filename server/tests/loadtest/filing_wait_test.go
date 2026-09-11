package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Filing waits for a row the register frame has not produced yet.
//
// A machine's row is written when the server has finished reading its register
// frame. The machine's own write returns as soon as the bytes are buffered
// locally and nothing comes back down the stream to say the row landed, so the
// filing that follows an arrival is in a race the machine cannot see it is in.

// Measured against the assembled product on an idle workstation, three filings
// in eight landed in that gap; on a nightly's half-processor target with a fleet
// arriving, it was every one of the first five, which is the give-up rule — so
// the run filed nothing at all and every leg waiting on a filed estate stood
// idle for the rest of the night.
func TestFilingWaitsForTheRowTheRegisterFrameHasNotProducedYet(t *testing.T) {
	filer, api := aFiler(t, 10, 10)
	api.rowLandsAfter = 2

	filer.file(context.Background(), aMachine(0))

	require.Len(t, api.filedToCustomer, 1, "the machine is filed once its row lands")
	filed, refused := filer.counts()
	assert.Equal(t, 1, filed)
	assert.Zero(t, refused, "a row that had not landed yet is not a refusal")
}

// The wait is bounded. A server that goes on saying it has no such machine is
// answering about something the run cannot fix by asking again, and every
// further attempt spends a request the arrivals need.
func TestFilingStopsAskingForAMachineTheServerNeverHas(t *testing.T) {
	filer, api := aFiler(t, 10, 10)
	api.rowLandsAfter = filingWaitAttempts + 10

	filer.file(context.Background(), aMachine(0))

	filed, refused := filer.counts()
	assert.Zero(t, filed)
	assert.Equal(t, 1, refused, "a machine the server never has is one the run could not file")
	assert.Equal(t, filingWaitAttempts, api.attemptsAtFiling(),
		"the wait is bounded rather than open-ended")
}

// What the server said is the difference between a machine whose row is missing
// and a customer that is not there, and both answer 404 on this path. The run
// that met it could say only that the answer was 404, which is why the cause
// took two nights of nightlies to find.
func TestARefusedCallCarriesWhatTheServerSaidAboutIt(t *testing.T) {
	fleet := buildFleet(t, FixtureLopsided, 3)
	fleet.api.rowLandsAfter = filingWaitAttempts + 10

	err := fleet.client.FileDevice(fleet.fixture, 0, 10, "device-0")

	require.Error(t, err)
	assert.ErrorContains(t, err, "device not found",
		"the refusal repeats the server's own words")
	assert.ErrorContains(t, err, "404", "and the status it answered with")
}
