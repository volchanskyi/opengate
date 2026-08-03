package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter(t *testing.T) {
	t.Parallel()
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("requests under limit pass", func(t *testing.T) {
		handler := RateLimiter(10, 5)(okHandler)
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "1.2.3.4:1234"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, "request %d should pass", i)
		}
	})

	t.Run("requests over limit return 429", func(t *testing.T) {
		handler := RateLimiter(1, 2)(okHandler)
		var codes []int
		for i := 0; i < 10; i++ {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "5.6.7.8:1234"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			codes = append(codes, rec.Code)
		}
		has429 := false
		for _, c := range codes {
			if c == http.StatusTooManyRequests {
				has429 = true
				break
			}
		}
		require.True(t, has429, "expected at least one 429 response, got %v", codes)
	})

	t.Run("different IPs get independent limits", func(t *testing.T) {
		handler := RateLimiter(1, 1)(okHandler)

		// Exhaust limit for IP A
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "10.0.0.1:1234"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}

		// IP B should still be able to make a request
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.2:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("X-Forwarded-For from a trusted proxy identifies the client", func(t *testing.T) {
		handler := RateLimiter(1, 1)(okHandler)

		// First request through the proxy should pass
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		// Second request from the same client should be limited
		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	})

	// A client able to mint a fresh bucket per request by varying a header it
	// controls would have no rate limit at all, so a varying X-Forwarded-For
	// must never move the client's identity — whether the peer is an untrusted
	// public address or the trusted ingress whose appended entry the attacker
	// prepends to.
	bypass := []struct {
		name        string
		remoteAddr  string
		first, next string
	}{
		{"untrusted peer", "203.0.113.50:44321", "198.51.100.1", "198.51.100.2"},
		{"prepended to the ingress entry", "10.0.0.1:1234", "198.51.100.1, 203.0.113.7", "198.51.100.2, 203.0.113.7"},
	}
	for _, tt := range bypass {
		t.Run("varying X-Forwarded-For cannot mint fresh buckets: "+tt.name, func(t *testing.T) {
			handler := RateLimiter(1, 1)(okHandler)
			send := func(xff string) int {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.RemoteAddr = tt.remoteAddr
				req.Header.Set("X-Forwarded-For", xff)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				return rec.Code
			}
			assert.Equal(t, http.StatusOK, send(tt.first))
			assert.Equal(t, http.StatusTooManyRequests, send(tt.next),
				"a different X-Forwarded-For must not reset the limit")
		})
	}
}

func TestExtractIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{"remote addr with port", "1.2.3.4:5678", "", "1.2.3.4"},
		{"remote addr without port", "1.2.3.4", "", "1.2.3.4"},
		// Loopback and private peers are our own reverse proxy: the entry it
		// appended — the last one — is the client it actually observed.
		{"xff single from loopback proxy", "127.0.0.1:80", "203.0.113.1", "203.0.113.1"},
		{"xff from loopback proxy uses last hop", "127.0.0.1:80", "203.0.113.1, 198.51.100.9", "198.51.100.9"},
		{"xff with spaces", "127.0.0.1:80", " 203.0.113.2 , 198.51.100.8 ", "198.51.100.8"},
		{"xff from private proxy", "10.0.0.1:8080", "203.0.113.1, 198.51.100.7", "198.51.100.7"},
		// A public peer is not our proxy, so nothing it claims is trusted.
		{"xff ignored from public peer", "203.0.113.50:44321", "198.51.100.1", "203.0.113.50"},
		{"xff ignored from public peer with chain", "203.0.113.50:44321", "1.1.1.1, 2.2.2.2", "203.0.113.50"},
		// A malformed trailing entry must not become the identity.
		{"non-ip xff entry falls back to peer", "127.0.0.1:80", "not-an-ip", "127.0.0.1"},
		{"empty xff falls back to peer", "127.0.0.1:80", "   ", "127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			assert.Equal(t, tt.want, extractIP(req))
		})
	}
}
