package api

import (
	"context"
)

// clientErrorFieldCap bounds each logged field so one oversized report cannot flood the log.
const clientErrorFieldCap = 512

// ReportClientError logs a browser-side error report. The endpoint is unauthenticated, so
// the body is size-bounded and each field truncated; reports must carry no token or PII.
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
