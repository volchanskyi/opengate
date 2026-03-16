package api

import (
	"bytes"
	"context"
	_ "embed"
)

//go:embed install.sh
var installScript []byte

// GetInstallScript implements StrictServerInterface.
func (s *Server) GetInstallScript(_ context.Context, _ GetInstallScriptRequestObject) (GetInstallScriptResponseObject, error) {
	return GetInstallScript200TextxShellscriptResponse{
		Body:          bytes.NewReader(installScript),
		ContentLength: int64(len(installScript)),
	}, nil
}
