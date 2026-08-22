package main

import (
	"context"
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
