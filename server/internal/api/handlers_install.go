package api

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
)

//go:embed install.sh
var installScript []byte

// GetInstallScript implements StrictServerInterface.
func (s *Server) GetInstallScript(ctx context.Context, _ GetInstallScriptRequestObject) (GetInstallScriptResponseObject, error) {
	script := installScript

	// Inject the server URL so the script doesn't need to guess it from
	// /proc/$PPID/cmdline (which fails when piped through sudo).
	serverURL := s.baseURL
	if serverURL == "" {
		if r := httpRequestFromContext(ctx); r != nil {
			scheme := "https"
			if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
				scheme = proto
			}
			host := r.Header.Get("X-Forwarded-Host")
			if host == "" {
				host = r.Host
			}
			if host != "" {
				serverURL = fmt.Sprintf("%s://%s", scheme, host)
			}
		}
	}

	if serverURL != "" {
		prefix := []byte(fmt.Sprintf("# Injected by server\nexport OPENGATE_SERVER=%q\n\n", serverURL))
		script = append(prefix, installScript...)
	}

	return GetInstallScript200TextxShellscriptResponse{
		Body:          bytes.NewReader(script),
		ContentLength: int64(len(script)),
	}, nil
}
