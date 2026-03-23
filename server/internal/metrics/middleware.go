package metrics

import (
	"bufio"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// HTTPMiddleware returns a chi-compatible middleware that records request
// count and duration metrics. It uses chi's RoutePattern for the route label
// to avoid cardinality explosion from path parameters.
func HTTPMiddleware(m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}

			method := r.Method
			status := strconv.Itoa(sw.status)
			duration := time.Since(start).Seconds()

			m.HTTPRequestsTotal.WithLabelValues(method, route, status).Inc()
			m.HTTPRequestDuration.WithLabelValues(method, route).Observe(duration)
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// WriteHeader captures the status code and forwards exactly once.
func (sw *statusWriter) WriteHeader(code int) {
	if sw.wroteHeader {
		return
	}
	sw.status = code
	sw.wroteHeader = true
	sw.ResponseWriter.WriteHeader(code)
}

// Write calls WriteHeader(200) implicitly if not already called.
func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.wroteHeader = true
	}
	return sw.ResponseWriter.Write(b)
}

// Hijack implements http.Hijacker so WebSocket upgrades work through the metrics middleware.
func (sw *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := sw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap returns the underlying ResponseWriter for middleware compatibility.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}
