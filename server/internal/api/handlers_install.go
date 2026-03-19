package api

import (
	"bytes"
	"context"
	_ "embed" // Required for //go:embed directive to embed install.sh into the binary.
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

	var prefix []byte
	if serverURL != "" {
		prefix = append(prefix, []byte(fmt.Sprintf("export OPENGATE_SERVER=%q\n", serverURL))...)
	}
	if s.githubRepo != "" {
		prefix = append(prefix, []byte(fmt.Sprintf("export OPENGATE_GITHUB_REPO=%q\n", s.githubRepo))...)
	}
	if len(prefix) > 0 {
		header := append([]byte("# Injected by server\n"), prefix...)
		header = append(header, '\n')
		script = append(header, installScript...)
	}

	return GetInstallScript200TextxShellscriptResponse{
		Body:          bytes.NewReader(script),
		ContentLength: int64(len(script)),
	}, nil
}
