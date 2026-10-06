package api

import "context"

// GetHealth implements StrictServerInterface as the readiness probe: it reports 503 when
// Postgres or the wired session registry is unreachable, while /healthz stays dependency-free.
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
