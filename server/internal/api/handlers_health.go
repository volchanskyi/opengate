package api

import "context"

// GetHealth implements StrictServerInterface.
func (s *Server) GetHealth(ctx context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	if s.store.Ping(ctx) != nil {
		return GetHealth503JSONResponse{Error: "database unreachable"}, nil
	}
	return GetHealth200JSONResponse{Status: "ok"}, nil
}
