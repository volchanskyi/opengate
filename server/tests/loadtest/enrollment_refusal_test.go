package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refusingServer answers every enrollment with one status and body.
func refusingServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// enrollAgainst asks one machine to enroll and returns whatever came back.
func enrollAgainst(t *testing.T, server *httptest.Server) error {
	t.Helper()
	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:         server.URL,
		EnrollmentToken: "enroll-token",
		DeviceID:        "55555555-5555-5555-5555-555555555555",
	})
	return err
}

func TestOnlyTheStatusesTheServerChoosesAreRefusals(t *testing.T) {
	deliberate := []struct {
		status int
		body   string
		why    string
	}{
		{http.StatusNotFound, `{"error":"invalid enrollment token"}`, "a credential that was never valid"},
		{http.StatusGone, `{"error":"token exhausted"}`, "a spent credential"},
		{http.StatusTooManyRequests, `{"error":"rate limit exceeded"}`, "a rate past a ceiling it enforces on purpose"},
	}
	for _, refusal := range deliberate {
		t.Run(refusal.why, func(t *testing.T) {
			err := enrollAgainst(t, refusingServer(t, refusal.status, refusal.body))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrEnrollmentRefused, refusal.why)
		})
	}
}

func TestAServerThatBrokeIsNotAServerRefusing(t *testing.T) {
	broken := []struct {
		status int
		body   string
		why    string
	}{
		{http.StatusBadRequest, `{"error":"invalid enrollment request"}`, "the harness sent something wrong"},
		{http.StatusInternalServerError, `{"error":"internal"}`, "the server broke"},
		{http.StatusBadGateway, `bad gateway`, "nothing answered behind the edge"},
		{http.StatusServiceUnavailable, `{"error":"request timeout"}`, "the reading a red night actually took"},
	}
	for _, failure := range broken {
		t.Run(failure.why, func(t *testing.T) {
			err := enrollAgainst(t, refusingServer(t, failure.status, failure.body))
			require.Error(t, err)
			assert.NotErrorIs(t, err, ErrEnrollmentRefused, failure.why)
			assert.ErrorIs(t, err, ErrEnrollmentFailed,
				"a server that broke is still a machine that did not get in")
		})
	}
}

func TestARefusalNamesTheStatusItCameBackWith(t *testing.T) {
	err := enrollAgainst(t, refusingServer(t, http.StatusTooManyRequests, `{"error":"rate limit exceeded"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

// fleetOf drives one fleet through a fixed list of outcomes and returns what it
// tallied. A machine says it arrived only when it reached registered.
func fleetOf(t *testing.T, outcomes []agentResult) FleetOutcomes {
	t.Helper()
	var next atomic.Int64
	fleet := NewQUICFleet(func(_ context.Context, _ int, presence fleetPresence) agentResult {
		result := outcomes[next.Add(1)-1]
		if !result.arrivedAt.IsZero() {
			presence.Arrived()
		}
		return result
	})
	require.NoError(t, fleet.HoldConnected(0, len(outcomes)))
	fleet.Stop()
	return fleet.Outcomes()
}

func registered() agentResult {
	return agentResult{connectDur: time.Millisecond, arrivedAt: time.Now()}
}

func TestAFleetRefusedAtADeclaredCeilingReportsNoErrors(t *testing.T) {
	tally := fleetOf(t, []agentResult{
		registered(),
		registered(),
		{err: ErrEnrollmentRefused},
		{err: ErrEnrollmentRefused},
	})

	assert.EqualValues(t, 2, tally.Arrived)
	assert.EqualValues(t, 0, tally.Failed, "a refusal the server made on purpose is not a failure to arrive")
	assert.EqualValues(t, 2, tally.Rejected)
	assert.InDelta(t, 0.0, tally.ErrorRate(), 0.0001, "the limit working is not an error rate")
}

func TestAFleetThatMetABrokenServerReportsEveryOne(t *testing.T) {
	tally := fleetOf(t, []agentResult{
		registered(),
		{err: errors.New("enroll: " + ErrEnrollmentFailed.Error() + " with 503: request timeout")},
		{err: ErrEnrollmentFailed},
		{err: ErrEnrollmentFailed},
	})

	assert.EqualValues(t, 1, tally.Arrived)
	assert.EqualValues(t, 3, tally.Failed, "a server that would not answer is three machines that did not get in")
	assert.EqualValues(t, 0, tally.Rejected)
	assert.InDelta(t, 0.75, tally.ErrorRate(), 0.0001)
}

func TestAStoodDownMachineIsNeitherArrivedNorRefused(t *testing.T) {
	tally := fleetOf(t, []agentResult{
		registered(),
		{err: context.Canceled},
	})

	assert.EqualValues(t, 1, tally.Arrived)
	assert.EqualValues(t, 0, tally.Failed)
	assert.EqualValues(t, 0, tally.Rejected)
	assert.EqualValues(t, 1, tally.StoodDown)
}

func TestAnErrorRateOfZeroSaysWhetherAnythingWasMeasured(t *testing.T) {
	clean := fleetOf(t, []agentResult{registered(), registered()})
	assert.InDelta(t, 0.0, clean.ErrorRate(), 0.0001)
	assert.True(t, clean.Measured(), "two machines arrived, so the zero is a reading")

	refused := fleetOf(t, []agentResult{
		{err: ErrEnrollmentRefused},
		{err: ErrEnrollmentRefused},
	})
	assert.EqualValues(t, 0, refused.Attempted())
	assert.InDelta(t, 0.0, refused.ErrorRate(), 0.0001)
	assert.False(t, refused.Measured(), "every machine was refused, so the zero is not a reading")
	assert.EqualValues(t, 2, refused.Rejected, "and the run still says what became of them")
}

func TestTheRejectionCountReachesTheBundle(t *testing.T) {
	bundle := bundleFrom(t, []agentResult{
		registered(),
		registered(),
		{err: ErrEnrollmentRefused},
	}, true)

	value, published := observedValue(bundle, "aggregate_rejected")
	require.True(t, published, "a run refused at a ceiling has to look different from one that was not")
	assert.InDelta(t, 1.0, value, 0.0001)

	rate, published := observedValue(bundle, "aggregate_error_rate")
	require.True(t, published)
	assert.InDelta(t, 0.0, rate, 0.0001,
		"a machine a declared ceiling refused is out of the denominator as well as the numerator")
}
