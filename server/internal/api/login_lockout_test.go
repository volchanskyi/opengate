package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginHandlerPerEmailLockout(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	const victim = "victim@example.com"
	seedTestUser(t, srv, cfg, victim, false)

	// The peer address identifies a client, so the test varies RemoteAddr, not a forgeable header.
	failFromIP := func(email, ip string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]string{"email": email, "password": "wrong"})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, testPathLogin, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = net.JoinHostPort(ip, "44321")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}

	for i := 0; i < loginMaxFailures; i++ {
		w := failFromIP(victim, fmt.Sprintf("203.0.113.%d", i))
		require.Equal(t, http.StatusUnauthorized, w.Code, "attempt %d should be 401", i)
	}

	assert.Equal(t, http.StatusTooManyRequests, failFromIP(victim, "198.51.100.7").Code,
		"account should be locked after the failure threshold")
	assert.Equal(t, http.StatusUnauthorized, failFromIP("bystander@example.com", "198.51.100.8").Code,
		"an unrelated account must not be locked")
}
