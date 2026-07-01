package api

import (
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	w := doRequest(srv, http.MethodGet, "/api/v1/health", "", nil)
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
	assert.Equal(t, "max-age=63072000; includeSubDomains; preload", w.Header().Get("Strict-Transport-Security"))
}

func TestRequestTimeout(t *testing.T) {
	t.Parallel()
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			return
		}
	})

	handler := RequestTimeout(50 * time.Millisecond)(slow)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestMaxBodySize(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "bodysize@example.com", false)

	t.Run("small body accepted", func(t *testing.T) {
		body := map[string]string{"name": "test-group"}
		w := doRequest(srv, http.MethodPost, "/api/v1/groups", token, body)
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("oversized body rejected", func(t *testing.T) {
		// Create a body larger than 1 MB.
		huge := make([]byte, maxRequestBodySize+1)
		for i := range huge {
			huge[i] = 'a'
		}
		w := doRawRequest(srv, http.MethodPost, "/api/v1/groups", token, string(huge))
		assert.True(t, w.Code == http.StatusBadRequest || w.Code == http.StatusRequestEntityTooLarge,
			"expected 400 or 413, got %d", w.Code)
	})
}
