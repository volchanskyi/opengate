package api

import (
	"context"
)

// clientErrorFieldCap bounds each logged field so a malicious or buggy client
// cannot flood the server log with a single oversized report. The OpenAPI spec
// also caps the fields, but this is the defense-in-depth at the sink.
const clientErrorFieldCap = 512

// ReportClientError records a browser-side error report in the server log so
// production frontend crashes are observable (Loki). The endpoint is
// unauthenticated and rate-limited by the global limiter; the body is bounded
// by MaxBodySize and each field is truncated before logging. Reports must carry
// no token, credential, or PII — callers are responsible for that, and the
// fields are logged verbatim (after truncation) only for debugging.
func (s *Server) ReportClientError(_ context.Context, request ReportClientErrorRequestObject) (ReportClientErrorResponseObject, error) {
	if request.Body == nil || request.Body.Message == "" {
		return ReportClientError400JSONResponse{Error: "message is required"}, nil
	}

	attrs := []any{"message", truncate(request.Body.Message, clientErrorFieldCap)}
	if v := request.Body.Source; v != nil {
		attrs = append(attrs, "source", truncate(*v, clientErrorFieldCap))
	}
	if v := request.Body.Url; v != nil {
		attrs = append(attrs, "url", truncate(*v, clientErrorFieldCap))
	}
	if v := request.Body.UserAgent; v != nil {
		attrs = append(attrs, "user_agent", truncate(*v, clientErrorFieldCap))
	}
	if v := request.Body.Stack; v != nil {
		attrs = append(attrs, "stack", truncate(*v, clientErrorFieldCap))
	}

	s.logger.Warn("client error", attrs...)
	return ReportClientError204Response{}, nil
}

// truncate shortens s to at most n bytes, appending an ellipsis marker when cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
