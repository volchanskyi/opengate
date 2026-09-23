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

// A limit the server enforced on purpose, and a server that fell over, told
// apart — and the order the two halves of that have to land in.
//
// The harness said two contradictory things about a refusal. The comment above
// the tally says a refusal "is held apart from both", and the tally put it in
// the failure count and then also tagged it, so the rejection was a label on top
// of a failure and the error rate counted it. Underneath, the enrollment call
// returned that same refusal for *any* non-200 — so a 500, a 502 and the
// `503 request timeout` a night actually saw were all labelled "the server
// declining on purpose".
//
// The two defects cancel. Because refusals still landed in the failure count, a
// server falling over was caught — by accident. Moving the refusal out without
// first narrowing what counts as one takes every 5xx out of the error rate with
// it, and a server that had stopped answering would report a perfect run.

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

// What the enrollment endpoint deliberately answers with. Read off the handler
// and the rate limiter rather than assumed: the endpoint is unauthenticated, so
// it never answers 401 or 403, and a test written against those would pin
// behaviour the product does not have.
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

// And what it does not choose. A bad signing request is the harness sending
// something wrong, and a 5xx is the server broken — neither is a limit working.
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

// The status is named whichever of the two it was, because a night reading the
// log is the only place the difference is visible.
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

// A fleet refused at a ceiling it declared reports no errors. The limit worked;
// counting it as a defect makes a correctly enforced limit look broken and
// buries the real failures underneath it.
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

// And a fleet that met a server which had stopped answering reports all of it.
// This is the half that must land in the same change: on its own, the one above
// takes every 5xx out of the error rate with it.
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

// A machine the run itself stood down never asked the system anything, and it
// stays out of both — which it already did, and which the change must not move.
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

// Taking the refusal out of the failure count takes it out of the denominator
// too, and that produces a second zero that reads exactly like the first.
//
// A fleet every machine of which was refused at a ceiling has attempted nothing
// by this count, so the error rate comes back as zero through the guard against
// dividing by nothing rather than through anything having been measured. A
// reader cannot tell that from a clean run, so the tally says which it is.
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

// The count of what the ceiling refused reaches a reader. It had been recorded
// and then travelled no further than one field, so a run refused at a limit and
// a run that was not looked identical in everything a night prints.
func TestTheRejectionCountReachesTheBundle(t *testing.T) {
	bundle := bundleFrom(t, []agentResult{
		registered(),
		registered(),
		{err: ErrEnrollmentRefused},
	}, true)

	value, published := observedValue(bundle, "aggregate_rejected")
	require.True(t, published, "a run refused at a ceiling has to look different from one that was not")
	assert.InDelta(t, 1.0, value, 0.0001)

	// And the error rate beside it is of the two machines that were actually
	// asked, not of the three the run produced. The two numbers are only
	// readable together: one says a limit turned somebody away, the other says
	// nothing went wrong with the machines that got through.
	rate, published := observedValue(bundle, "aggregate_error_rate")
	require.True(t, published)
	assert.InDelta(t, 0.0, rate, 0.0001,
		"a machine a declared ceiling refused is out of the denominator as well as the numerator")
}
