package api

import (
	"net/http"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// machinesWaitingFlag stands in for the sign-in line, saying whether machines wait their turn.
type machinesWaitingFlag struct{ waiting atomic.Bool }

func (m *machinesWaitingFlag) MachinesWaiting() bool { return m.waiting.Load() }

// consoleLineServer is a server whose console line is full, with machines waiting as told.
func consoleLineServer(t *testing.T, machinesWaiting bool) (*Server, *machinesWaitingFlag, string) {
	t.Helper()
	machines := &machinesWaitingFlag{}
	machines.waiting.Store(machinesWaiting)
	cfg := testServerConfig(t)
	cfg.MachineLine = machines
	cfg.RequestTimeout = 1500 * time.Millisecond
	srv := NewServer(cfg)
	_, token := seedTestUser(t, srv, cfg.JWT, "technician@console-line.test", true)
	for range cap(srv.consoleLine.places) {
		srv.consoleLine.places <- struct{}{}
	}
	return srv, machines, token
}

func TestASignedInRequestWaitingPastItsTimeIsAskedToTryAgainShortly(t *testing.T) {
	srv, _, token := consoleLineServer(t, true)

	started := time.Now()
	w := doRequest(srv, http.MethodGet, "/api/v1/devices", token, nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"), "the answer says when to try again")
	assert.Less(t, time.Since(started), 1500*time.Millisecond,
		"the line answers before the request's own time runs out")
}

func TestASignedInRequestRunsAtOnceWhileNoMachineWaits(t *testing.T) {
	srv, _, token := consoleLineServer(t, false)

	w := doRequest(srv, http.MethodGet, "/api/v1/devices", token, nil)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAWaitingRequestGoesAheadOnceNoMachineWaits(t *testing.T) {
	srv, machines, token := consoleLineServer(t, true)

	answered := make(chan int, 1)
	go func() { answered <- doRequest(srv, http.MethodGet, "/api/v1/devices", token, nil).Code }()
	select {
	case code := <-answered:
		t.Fatalf("the request was answered %d while machines waited and every place was taken", code)
	case <-time.After(300 * time.Millisecond):
	}
	machines.waiting.Store(false)

	assert.Equal(t, http.StatusOK, <-answered)
}

func TestAWaitingRequestTakesThePlaceAFinishedOneGivesBack(t *testing.T) {
	srv, _, token := consoleLineServer(t, true)

	answered := make(chan int, 1)
	go func() { answered <- doRequest(srv, http.MethodGet, "/api/v1/devices", token, nil).Code }()
	select {
	case code := <-answered:
		t.Fatalf("the request was answered %d before a place came free", code)
	case <-time.After(300 * time.Millisecond):
	}
	<-srv.consoleLine.places

	assert.Equal(t, http.StatusOK, <-answered)
}

func TestAMachineEnrolmentNeverWaitsInTheConsoleLine(t *testing.T) {
	srv, _, _ := consoleLineServer(t, true)

	started := time.Now()
	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/not-a-token", "", map[string]string{"hostname": "laptop-7"})

	assert.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	assert.Less(t, time.Since(started), 500*time.Millisecond)
}

func TestTheConsoleLineHoldsAPlacePerShareOfTheProcessor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		share float64
		want  int
	}{
		{"a quarter processor holds at least one place", 0.25, max(1, int(consoleRequestsPerProcessor/4))},
		{"a tenth of a processor still holds one place", 0.1, 1},
		{"a negative share counts every processor of the machine", -1, runtime.NumCPU() * consoleRequestsPerProcessor},
		{"two processors hold twice the per-processor number", 2, 2 * consoleRequestsPerProcessor},
		{"an unknown share counts every processor of the machine", 0, runtime.NumCPU() * consoleRequestsPerProcessor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := newConsoleLine(&machinesWaitingFlag{}, tc.share)
			require.NotNil(t, line)
			assert.Equal(t, tc.want, cap(line.places))
		})
	}
}
