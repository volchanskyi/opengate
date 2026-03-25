package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter(t *testing.T) {
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

	t.Run("X-Forwarded-For is used when present", func(t *testing.T) {
		handler := RateLimiter(1, 1)(okHandler)

		// First request with XFF should pass
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		// Second request from same XFF should be limited
		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	})
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{"remote addr with port", "1.2.3.4:5678", "", "1.2.3.4"},
		{"remote addr without port", "1.2.3.4", "", "1.2.3.4"},
		{"xff single", "127.0.0.1:80", "203.0.113.1", "203.0.113.1"},
		{"xff multiple", "127.0.0.1:80", "203.0.113.1, 10.0.0.1", "203.0.113.1"},
		{"xff with spaces", "127.0.0.1:80", " 203.0.113.2 , 10.0.0.1", "203.0.113.2"},
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
