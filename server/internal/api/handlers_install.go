package api

import (
	"bytes"
	"context"
	_ "embed" // embeds install.sh into the binary.
	"fmt"
	"regexp"
	"strings"
)

//go:embed install.sh
var installScript []byte

// installHostRE matches a DNS name, IPv4 or bracketed IPv6 literal with an optional port; the
// host is emitted into a script piped to `sudo bash`, so anything else would run as root.
var installHostRE = regexp.MustCompile(
	`^(\[[0-9A-Fa-f:.]{2,45}\]|[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?)(:[0-9]{1,5})?$`)

// shellSingleQuote renders s as a single-quoted POSIX shell word, inert to expansion, with
// embedded quotes closed, escaped and reopened.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// deriveInstallServerURL prefers operator configuration, then caller-controlled request headers
// checked for an http/https scheme and installHostRE host; false makes the installer discover.
func (s *Server) deriveInstallServerURL(ctx context.Context) (string, bool) {
	if s.baseURL != "" {
		return s.baseURL, true
	}

	r := httpRequestFromContext(ctx)
	if r == nil {
		return "", false
	}

	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	if scheme != "http" && scheme != "https" {
		return "", false
	}

	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if !installHostRE.MatchString(host) {
		return "", false
	}

	return scheme + "://" + host, true
}

// GetInstallScript implements StrictServerInterface.
func (s *Server) GetInstallScript(ctx context.Context, _ GetInstallScriptRequestObject) (GetInstallScriptResponseObject, error) {
	script := installScript

	// The injected URL spares the script from reading /proc/$PPID/cmdline, which fails under sudo.
	var prefix []byte
	if serverURL, ok := s.deriveInstallServerURL(ctx); ok {
		prefix = fmt.Appendf(prefix, "export OPENGATE_SERVER=%s\n", shellSingleQuote(serverURL))
	}
	if s.githubRepo != "" {
		prefix = fmt.Appendf(prefix, "export OPENGATE_GITHUB_REPO=%s\n", shellSingleQuote(s.githubRepo))
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
