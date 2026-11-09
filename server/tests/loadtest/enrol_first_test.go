package main

import (
	"context"
	"crypto/tls"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refusingOne refuses one machine by name and enrols every other.
type refusingOne struct {
	countingSource
	refused string
}

func (s *refusingOne) forAgent(ctx context.Context, plan tenantAgent) (*tls.Config, error) {
	if plan.hostname == s.refused {
		return nil, ErrEnrollmentRefused
	}
	return s.countingSource.forAgent(ctx, plan)
}

// registeringLife enrols a machine and, as the server does, writes its row a moment after the
// registration only while the connection is still open; a nil server writes nothing.
func registeringLife(credentials agentCredentials, api *fakeAPI) installFunc {
	return func(ctx context.Context, machine tenantAgent, whileConnected func()) error {
		config, err := credentials.forAgent(ctx, machine)
		if err != nil {
			return err
		}
		id, _ := deviceIDFrom(config)
		var open atomic.Bool
		open.Store(true)
		written := make(chan struct{})
		go func() {
			defer close(written)
			time.Sleep(20 * time.Millisecond)
			if open.Load() && api != nil {
				api.land(id)
			}
		}()
		whileConnected()
		open.Store(false)
		<-written
		return nil
	}
}

func TestEveryPlannedMachineLivesOnceBeforeTheWalkAndDialsWithWhatItHolds(t *testing.T) {
	source := &countingSource{}
	credentials := enrolOnce(source)
	plan := planAgents(20, 2, "soak")

	count := enrolFirst(context.Background(), plan, registeringLife(credentials, nil), nil, 4)

	assert.Equal(t, 20, count.Installed)
	assert.Zero(t, count.Failed)
	require.Len(t, source.askedFor(), 20, "each machine enrols once")

	for _, machine := range plan {
		_, err := credentials.forAgent(context.Background(), machine)
		require.NoError(t, err)
	}
	assert.Len(t, source.askedFor(), 20, "the walk's dial is answered from the held certificate")
}

func TestInstallingFirstHoldsNoMoreThanItsShareInFlight(t *testing.T) {
	var mu sync.Mutex
	current, most := 0, 0
	install := func(context.Context, tenantAgent, func()) error {
		mu.Lock()
		current++
		most = max(most, current)
		mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		mu.Lock()
		current--
		mu.Unlock()
		return nil
	}

	count := enrolFirst(context.Background(), planAgents(40, 1, "soak"), install, nil, 3)

	assert.Equal(t, 40, count.Installed)
	mu.Lock()
	defer mu.Unlock()
	assert.LessOrEqual(t, most, 3, "each machine waits for its answer, a few at a time")
	assert.Positive(t, most)
}

func TestAMachineRefusedBeforeTheWalkIsCountedAndAskedAgainOnItsDial(t *testing.T) {
	plan := planAgents(5, 1, "soak")
	source := &refusingOne{refused: plan[2].hostname}
	credentials := enrolOnce(source)

	count := enrolFirst(context.Background(), plan, registeringLife(credentials, nil), nil, 2)

	assert.Equal(t, 4, count.Installed, "one refusal leaves the rest installed")
	assert.Equal(t, 1, count.Failed)

	source.refused = ""
	_, err := credentials.forAgent(context.Background(), plan[2])
	require.NoError(t, err, "the refused machine enrols again when it dials")
}

func TestEachMachineIsFiledWhileItsFirstConnectionIsOpen(t *testing.T) {
	filer, api := aFiler(t, 3, 3)
	api.registerOnly()
	plan := planAgents(3, 1, "soak")

	count := enrolFirst(context.Background(), plan, registeringLife(filer.credentials, api), filer, 3)

	assert.Equal(t, 3, count.Installed)
	filed, refused := filer.counts()
	assert.Equal(t, 3, filed, "the estate is filed before the walk, its rows written while connected")
	assert.Zero(t, refused)
	assert.Len(t, api.filedToCustomer, 3)

	for _, machine := range plan {
		filer.file(context.Background(), machine)
	}
	assert.Len(t, api.filedToCustomer, 3, "an arrival in the walk files nothing again")
}

func TestAMachineWhoseFirstConnectionFailedIsLeftToFileOnArrival(t *testing.T) {
	filer, api := aFiler(t, 3, 3)
	api.registerOnly()
	failing := func(context.Context, tenantAgent, func()) error { return ErrEnrollmentFailed }

	count := enrolFirst(context.Background(), planAgents(3, 1, "soak"), failing, filer, 2)

	assert.Equal(t, 3, count.Failed)
	assert.Zero(t, api.attemptsAtFiling(), "a machine with no row is never offered for filing")
}

func TestInstallingFirstStopsWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	credentials := enrolOnce(&countingSource{})

	count := enrolFirst(ctx, planAgents(50, 1, "soak"), registeringLife(credentials, nil), nil, 4)

	assert.Zero(t, count.Installed, "an ended run installs nobody")
}

func TestInstallingFirstWithNoShareGivenStillInstallsOneAtATime(t *testing.T) {
	credentials := enrolOnce(&countingSource{})

	count := enrolFirst(context.Background(), planAgents(5, 1, "soak"), registeringLife(credentials, nil), nil, 0)

	assert.Equal(t, 5, count.Installed)
}

func TestAnEmptyEstateInstallsNothingAndReturns(t *testing.T) {
	credentials := enrolOnce(&countingSource{})

	count := enrolFirst(context.Background(), nil, registeringLife(credentials, nil), nil, 4)

	assert.Zero(t, count.Installed)
	assert.Zero(t, count.Failed)
}
