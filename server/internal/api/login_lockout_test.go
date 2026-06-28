package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoginHandlerPerEmailLockout verifies that repeated failed logins for one
// account lock the login path with a 429, regardless of source IP, while a
// different account stays unaffected.
func TestLoginHandlerPerEmailLockout(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	const victim = "victim@example.com"
	seedTestUser(t, srv, cfg, victim, false)

	// failFromIP submits a wrong-password login for email from a distinct IP.
	failFromIP := func(email, ip string) *httptest.ResponseRecorder {
		body := map[string]string{"email": email, "password": "wrong"}
		return doRequestWithHeaders(srv, http.MethodPost, testPathLogin, "", body,
			map[string]string{"X-Forwarded-For": ip})
	}

	// Spread failures across distinct IPs — the per-IP limiter would not trip.
	for i := 0; i < loginMaxFailures; i++ {
		w := failFromIP(victim, fmt.Sprintf("203.0.113.%d", i))
		require.Equal(t, http.StatusUnauthorized, w.Code, "attempt %d should be 401", i)
	}

	assert.Equal(t, http.StatusTooManyRequests, failFromIP(victim, "198.51.100.7").Code,
		"account should be locked after the failure threshold")
	assert.Equal(t, http.StatusUnauthorized, failFromIP("bystander@example.com", "198.51.100.8").Code,
		"an unrelated account must not be locked")
}
