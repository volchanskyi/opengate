package api

import "context"

// GetHealth implements StrictServerInterface — the readiness probe. It reports
// 503 (draining the pod) when a backing dependency is unreachable: Postgres, or
// the distributed session registry (Redis) when one is wired. The registry check
// is what drains a pod that has lost Redis (ADR-023 recovery posture); liveness
// (/healthz) stays dependency-free so the pod is not restarted.
func (s *Server) GetHealth(ctx context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	if s.store.Ping(ctx) != nil {
		return GetHealth503JSONResponse{Error: "database unreachable"}, nil
	}
	if s.relay != nil {
		if err := s.relay.PingRegistry(ctx); err != nil {
			return GetHealth503JSONResponse{Error: "session registry unreachable"}, nil
		}
	}
	return GetHealth200JSONResponse{Status: "ok"}, nil
}
