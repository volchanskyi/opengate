package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilingWaitsForTheRowTheRegisterFrameHasNotProducedYet(t *testing.T) {
	filer, api := aFiler(t, 10, 10)
	api.rowLandsAfter = 2

	filer.file(context.Background(), aMachine(0))

	require.Len(t, api.filedToCustomer, 1, "the machine is filed once its row lands")
	filed, refused := filer.counts()
	assert.Equal(t, 1, filed)
	assert.Zero(t, refused, "a row that had not landed yet is not a refusal")
}

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

func TestARefusedCallCarriesWhatTheServerSaidAboutIt(t *testing.T) {
	fleet := buildFleet(t, FixtureLopsided, 3)
	fleet.api.rowLandsAfter = filingWaitAttempts + 10

	err := fleet.client.FileDevice(fleet.fixture, 0, 10, "device-0")

	require.Error(t, err)
	assert.ErrorContains(t, err, "device not found",
		"the refusal repeats the server's own words")
	assert.ErrorContains(t, err, "404", "and the status it answered with")
}

func TestTheFilingWaitOutlastsARegistrationPastTenSeconds(t *testing.T) {
	var window time.Duration
	for attempt := 1; attempt < filingWaitAttempts; attempt++ {
		window += filingWaitDelay(attempt)
		if attempt > 1 {
			assert.Equal(t, 2*filingWaitDelay(attempt-1), filingWaitDelay(attempt),
				"each wait is twice the one before")
		}
	}
	assert.Equal(t, 6, filingWaitAttempts, "the request count is unchanged")
	assert.Greater(t, window, 10*time.Second, "the window covers a registration past ten seconds")
}
