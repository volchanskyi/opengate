package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// captureRequestLog runs RequestLogger over a GET to path and returns the
// emitted log text.
func captureRequestLog(path string) string {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, path, nil)
	RequestLogger(logger)(next).ServeHTTP(httptest.NewRecorder(), req)
	return buf.String()
}

// TestRequestLoggerRedaction asserts the relay-token segment is redacted in
// request logs while ordinary API paths are logged verbatim.
func TestRequestLoggerRedaction(t *testing.T) {
	t.Parallel()

	t.Run("redacts the relay token segment", func(t *testing.T) {
		t.Parallel()
		const token = "supersecretrelaytoken1234567890"
		out := captureRequestLog("/ws/relay/" + token)
		assert.NotContains(t, out, token, "full relay token must never be logged")
		assert.Contains(t, out, protocol.RedactToken(token))
	})

	t.Run("leaves non-relay paths intact", func(t *testing.T) {
		t.Parallel()
		assert.Contains(t, captureRequestLog("/api/v1/devices"), "/api/v1/devices")
	})
}
