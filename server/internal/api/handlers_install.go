package api

import (
	"bytes"
	"context"
	_ "embed" // Required for //go:embed directive to embed install.sh into the binary.
	"fmt"
	"regexp"
	"strings"
)

//go:embed install.sh
var installScript []byte

// installHostRE matches the host[:port] forms a deployment can legitimately
// present: a DNS name, an IPv4 literal, or a bracketed IPv6 literal, each with
// an optional port. Nothing outside that set is accepted — the matched value is
// emitted into a script the documented install flow pipes into `sudo bash`, so
// a host carrying shell metacharacters, whitespace, or a path would become root
// command execution on the machine running the installer.
var installHostRE = regexp.MustCompile(
	`^(\[[0-9A-Fa-f:.]{2,45}\]|[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?)(:[0-9]{1,5})?$`)

// shellSingleQuote renders s as a single-quoted POSIX shell word. The shell
// performs no expansion inside single quotes, so command substitution and
// parameter expansion in the value stay inert; an embedded quote is closed,
// escaped, and reopened.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// deriveInstallServerURL returns the base URL to bake into the installer and
// whether one could be derived at all.
//
// Operator configuration wins outright. Otherwise the value comes from request
// headers, which are caller-controlled on this unauthenticated endpoint, so the
// scheme must be http/https and the host must match installHostRE. Reporting
// false is the fail-safe: the installer then runs its own server discovery
// instead of trusting a value the caller chose.
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

	// Inject the server URL so the script doesn't need to guess it from
	// /proc/$PPID/cmdline (which fails when piped through sudo).
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
