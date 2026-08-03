package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// captureRequestLog runs RequestLogger over a GET to path and returns the
// emitted log text.
func captureRequestLog(path string) string {
	return captureRequestLogWithID(path, "")
}

// captureRequestLogWithID is captureRequestLog with chi's RequestID middleware
// in front. That middleware honours an inbound X-Request-Id, so passing a
// non-empty reqID makes the correlation value assertable end to end.
func captureRequestLogWithID(path, reqID string) string {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, path, nil)
	handler := RequestLogger(logger)(next)
	if reqID != "" {
		req.Header.Set("X-Request-Id", reqID)
		handler = middleware.RequestID(handler)
	}
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return buf.String()
}

// TestRequestLoggerRedaction asserts every path segment that is itself a
// bearer credential is redacted in request logs, while ordinary API paths are
// logged verbatim. Request logs ship to Loki, so a full token here crosses a
// trust boundary.
func TestRequestLoggerRedaction(t *testing.T) {
	t.Parallel()

	credentialPaths := map[string]string{
		"relay":      "/ws/relay/",
		"enrollment": "/api/v1/enroll/",
		"session":    "/api/v1/sessions/",
	}
	for name, prefix := range credentialPaths {
		t.Run("redacts the "+name+" token segment", func(t *testing.T) {
			t.Parallel()
			const token = "credentialsegmentabcdef1234567890"
			out := captureRequestLog(prefix + token)
			assert.NotContains(t, out, token, "full %s token must never be logged", name)
			assert.Contains(t, out, protocol.RedactToken(token))
		})
	}

	t.Run("leaves non-relay paths intact", func(t *testing.T) {
		t.Parallel()
		assert.Contains(t, captureRequestLog("/api/v1/devices"), "/api/v1/devices")
	})

	t.Run("leaves the collection paths intact", func(t *testing.T) {
		t.Parallel()
		assert.Contains(t, captureRequestLog("/api/v1/sessions"), "/api/v1/sessions")
		assert.Contains(t, captureRequestLog("/api/v1/enroll"), "/api/v1/enroll")
	})

	t.Run("leaves deeper sub-resource paths intact", func(t *testing.T) {
		t.Parallel()
		out := captureRequestLog("/api/v1/sessions/abc/extra")
		assert.Contains(t, out, "/api/v1/sessions/abc/extra")
	})
}

// TestRequestLoggerCorrelationID asserts the access log carries the chi request
// ID. Without it a multi-request incident — credential stuffing walking across
// endpoints, a session-hijack probe — cannot be stitched together in Loki.
func TestRequestLoggerCorrelationID(t *testing.T) {
	t.Parallel()

	t.Run("emits the request id assigned by the middleware", func(t *testing.T) {
		t.Parallel()
		const reqID = "correlation-test-id-1"
		out := captureRequestLogWithID("/api/v1/devices", reqID)
		assert.Contains(t, out, "request_id=")
		assert.Contains(t, out, reqID)
	})

	t.Run("omits an empty request id rather than logging a blank field", func(t *testing.T) {
		t.Parallel()
		// No RequestID middleware in the chain — nothing to correlate on.
		assert.NotContains(t, captureRequestLog("/api/v1/devices"), "request_id=")
	})
}
