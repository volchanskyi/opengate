package main

import (
	"context"
	"crypto/tls"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnEnrollmentURLMeansTheServerSigns(t *testing.T) {
	server := enrollmentServer(t)

	credentials, err := newAgentCredentials("", server.URL, "enroll-token")
	require.NoError(t, err)

	config, err := credentials.forAgent(context.Background(), tenantAgent{hostname: "soak-t0-a0"})
	require.NoError(t, err)
	require.Len(t, config.Certificates, 1)
	assert.NotNil(t, config.RootCAs, "an enrolled machine verifies the server with the authority it was handed")
}

func TestNoEnrollmentURLMeansTheHarnessSignsLocally(t *testing.T) {
	credentials, err := newAgentCredentials(t.TempDir(), "", "")
	require.NoError(t, err)

	config, err := credentials.forAgent(context.Background(), tenantAgent{hostname: "soak-t0-a0"})
	require.NoError(t, err)
	require.Len(t, config.Certificates, 1)
}

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

func TestNoCredentialSourceAtAllIsRefused(t *testing.T) {
	_, err := newAgentCredentials("", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate authority")
}

// countingSource is a credential source that records what it was asked for.
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
	// A fresh config per call, so a repeated pointer proves the memo answered.
	return &tls.Config{MinVersion: tls.VersionTLS13, ServerName: plan.hostname}, nil
}

func (s *countingSource) askedFor() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

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
