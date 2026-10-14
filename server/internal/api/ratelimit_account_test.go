package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

// healthAs sends one API request from addr, carrying token when it is set, and returns the status.
func healthAs(srv *Server, addr, token string) int {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.RemoteAddr = addr
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code
}

// sendUntilRefused spends the allowance of whatever bucket addr and token fall in.
func sendUntilRefused(t *testing.T, srv *Server, addr, token string) {
	t.Helper()
	for range 2000 {
		if healthAs(srv, addr, token) == http.StatusTooManyRequests {
			return
		}
	}
	t.Fatalf("2000 requests from %s were never refused", addr)
}

func accountToken(t *testing.T, cfg *auth.JWTConfig) string {
	t.Helper()
	token, err := cfg.GenerateToken(uuid.New(), "tech@northwind.test", false, uuid.New())
	require.NoError(t, err)
	return token
}

func TestAPIRequestsAreCountedUnderTheAccountThatMadeThem(t *testing.T) {
	srv, cfg := newTestServer(t)
	forger := &auth.JWTConfig{Secret: "another-secret-key-of-32-bytes-or-more", Issuer: cfg.Issuer, Duration: time.Hour}

	t.Run("forty technicians behind one office address open a machine at once", func(t *testing.T) {
		const office = "203.0.113.10:41000"
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			refused int
		)
		for range 40 {
			token := accountToken(t, cfg)
			wg.Go(func() {
				for range 6 {
					if healthAs(srv, office, token) == http.StatusTooManyRequests {
						mu.Lock()
						refused++
						mu.Unlock()
					}
				}
			})
		}
		wg.Wait()
		assert.Zero(t, refused, "240 requests from forty accounts share no allowance")
	})

	t.Run("one account past its allowance leaves another on the same address alone", func(t *testing.T) {
		const office = "203.0.113.20:41000"
		sendUntilRefused(t, srv, office, accountToken(t, cfg))
		assert.NotEqual(t, http.StatusTooManyRequests, healthAs(srv, office, accountToken(t, cfg)))
	})

	t.Run("a request without a token is counted under its address", func(t *testing.T) {
		const office = "203.0.113.30:41000"
		sendUntilRefused(t, srv, office, "")
		assert.NotEqual(t, http.StatusTooManyRequests, healthAs(srv, office, accountToken(t, cfg)),
			"a signed-in technician keeps an allowance of their own")
		assert.Equal(t, http.StatusTooManyRequests, healthAs(srv, office, ""))
	})

	t.Run("a token that fails validation is counted under its address", func(t *testing.T) {
		const office = "203.0.113.40:41000"
		forged := accountToken(t, forger)
		sendUntilRefused(t, srv, office, forged)
		assert.Equal(t, http.StatusTooManyRequests, healthAs(srv, office, ""),
			"a forged token spends the address's allowance")
		assert.Equal(t, http.StatusTooManyRequests, healthAs(srv, office, accountToken(t, forger)),
			"a fresh forged token mints no fresh allowance")
		assert.NotEqual(t, http.StatusTooManyRequests, healthAs(srv, office, accountToken(t, cfg)))
	})
}
