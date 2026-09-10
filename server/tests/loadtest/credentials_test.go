package main

import (
	"context"
	"crypto/tls"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Which source a run uses is a security decision, so it is made once and tested
// rather than repeated at each call site.

func TestAnEnrollmentURLMeansTheServerSigns(t *testing.T) {
	server := enrollmentServer(t)

	credentials, err := newAgentCredentials("", server.URL, "enroll-token")
	require.NoError(t, err)

	config, err := credentials.forAgent(context.Background(), tenantAgent{hostname: "soak-t0-a0"})
	require.NoError(t, err)
	require.Len(t, config.Certificates, 1)
	assert.NotNil(t, config.RootCAs, "an enrolled machine verifies the server with the authority it was handed")
}

// A local stack owns its own authority, and the harness signs against it. That
// is safe precisely because the stack is as disposable as the authority is.
func TestNoEnrollmentURLMeansTheHarnessSignsLocally(t *testing.T) {
	credentials, err := newAgentCredentials(t.TempDir(), "", "")
	require.NoError(t, err)

	config, err := credentials.forAgent(context.Background(), tenantAgent{hostname: "soak-t0-a0"})
	require.NoError(t, err)
	require.Len(t, config.Certificates, 1)
}

// Enrolling with no token would silently fall back to needing the authority
// key, which is the thing this exists to avoid.
func TestEnrollingWithoutATokenIsRefused(t *testing.T) {
	_, err := newAgentCredentials("", "http://localhost:8080", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
}

func TestAnEnrollmentURLOutsideTheAllowlistIsRefused(t *testing.T) {
	_, err := newAgentCredentials("", "http://opengate-server.opengate.svc.cluster.local:8080", "tok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed load-test target")
}

// With neither an authority to sign against nor a server to ask, there is
// nothing to dial with — and saying so is better than failing later with a
// message about a certificate.
func TestNoCredentialSourceAtAllIsRefused(t *testing.T) {
	_, err := newAgentCredentials("", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate authority")
}

// countingSource is a credential source that records what it was asked for, so
// a test can say how many identities a run actually minted.
type countingSource struct {
	mu    sync.Mutex
	asked []string
	fail  error
}

func (s *countingSource) forAgent(_ context.Context, plan tenantAgent) (*tls.Config, error) {
	s.mu.Lock()
	s.asked = append(s.asked, plan.hostname)
	failure := s.fail
	s.mu.Unlock()

	if failure != nil {
		return nil, failure
	}
	// A fresh config per call, so a test that gets the same pointer twice knows
	// it was the memo answering rather than a coincidence.
	return &tls.Config{MinVersion: tls.VersionTLS13, ServerName: plan.hostname}, nil
}

func (s *countingSource) askedFor() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

// A real machine enrols when it is installed and reconnects with the
// certificate it already holds. A harness that mints a fresh identity on every
// start turns a burst of reconnections into a burst of enrolments against a
// ceiling the server enforces on purpose, and grows the customer's fleet for as
// long as the run lasts.
func TestAMachineEnrolsOnceAndComesBackWithWhatItHolds(t *testing.T) {
	source := &countingSource{}
	credentials := enrolOnce(source)
	machine := tenantAgent{hostname: "soak-t0-a0"}

	first, err := credentials.forAgent(context.Background(), machine)
	require.NoError(t, err)
	again, err := credentials.forAgent(context.Background(), machine)
	require.NoError(t, err)

	assert.Same(t, first, again, "a machine that comes back dials with the identity it already holds")
	assert.Equal(t, []string{"soak-t0-a0"}, source.askedFor())
}

func TestTwoMachinesEnrolSeparately(t *testing.T) {
	source := &countingSource{}
	credentials := enrolOnce(source)

	_, err := credentials.forAgent(context.Background(), tenantAgent{hostname: "soak-t0-a0"})
	require.NoError(t, err)
	_, err = credentials.forAgent(context.Background(), tenantAgent{hostname: "soak-t0-a1"})
	require.NoError(t, err)

	assert.Equal(t, []string{"soak-t0-a0", "soak-t0-a1"}, source.askedFor())
}

// An enrolment that was refused is not an identity, so remembering it would
// hand every later start the same failure and give the run no way back. The
// refusal is reported and the next start asks again.
func TestARefusedEnrolmentIsNotRemembered(t *testing.T) {
	source := &countingSource{fail: ErrEnrollmentRefused}
	credentials := enrolOnce(source)
	machine := tenantAgent{hostname: "soak-t0-a0"}

	_, err := credentials.forAgent(context.Background(), machine)
	require.ErrorIs(t, err, ErrEnrollmentRefused)

	source.mu.Lock()
	source.fail = nil
	source.mu.Unlock()

	config, err := credentials.forAgent(context.Background(), machine)
	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Len(t, source.askedFor(), 2)
}

// A ramp starts its machines together and a wind-down and climb can ask for the
// same machine from two goroutines at once. Under -race this is the case where
// one machine would enrol twice.
func TestOneMachineAskedForAtOnceEnrolsOnce(t *testing.T) {
	source := &countingSource{}
	credentials := enrolOnce(source)
	machine := tenantAgent{hostname: "soak-t0-a0"}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = credentials.forAgent(context.Background(), machine)
		}()
	}
	wg.Wait()

	assert.Len(t, source.askedFor(), 1, "one machine is one enrolment however many starts asked for it at once")
}
