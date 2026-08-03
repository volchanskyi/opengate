package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// injectedPrefix returns the header block the server prepends to the embedded
// installer, i.e. everything before the script's own shebang. Assertions target
// this block because the embedded script legitimately contains the same shell
// metacharacters an injection would introduce.
func injectedPrefix(body string) string {
	prefix, _, found := strings.Cut(body, "#!/usr/bin/env bash")
	if !found {
		return body
	}
	return prefix
}

// fetchInstallScript serves the unauthenticated installer with the given Host
// and forwarding headers.
func fetchInstallScript(t *testing.T, srv *Server, host string, headers map[string]string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/install.sh", nil)
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

// TestGetInstallScriptRejectsHostInjection proves the unauthenticated installer
// endpoint never reflects an attacker-controlled host into the script it
// serves. The script is documented to run as `curl … | sudo bash`, so a host
// carrying shell metacharacters would otherwise execute as root on every
// machine that runs the installer.
func TestGetInstallScriptRejectsHostInjection(t *testing.T) {
	t.Parallel()
	const payload = "pwned"
	malicious := []struct {
		name string
		host string
	}{
		{"command substitution", "x$(touch /tmp/pwned)"},
		{"backtick substitution", "x`touch /tmp/pwned`"},
		{"quote break out", `x"; touch /tmp/pwned; echo "`},
		{"newline statement", "x\nexport EVIL=pwned"},
		{"semicolon chain", "host.example.com; touch /tmp/pwned"},
		{"parameter expansion", "${IFS}pwned.example.com"},
		{"path suffix", "example.com/pwned"},
		{"space separated", "example.com pwned"},
	}
	for _, tc := range malicious {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := newTestServerWithCert(t)

			for header, hostValue := range map[string]string{
				"X-Forwarded-Host": tc.host,
			} {
				body := fetchInstallScript(t, srv, "internal:8080",
					map[string]string{header: hostValue})
				prefix := injectedPrefix(body)
				assert.NotContains(t, prefix, payload,
					"%s payload reached the emitted script prefix: %q", header, prefix)
			}
		})
	}
}

// TestGetInstallScriptRejectsForwardedProtoInjection covers the scheme half of
// the derived URL, which is concatenated with the host before emission.
func TestGetInstallScriptRejectsForwardedProtoInjection(t *testing.T) {
	t.Parallel()
	for _, proto := range []string{
		"https\nexport EVIL=pwned",
		"$(touch /tmp/pwned)",
		"javascript",
		"file",
	} {
		t.Run(proto, func(t *testing.T) {
			t.Parallel()
			srv, _ := newTestServerWithCert(t)
			body := fetchInstallScript(t, srv, "internal:8080", map[string]string{
				"X-Forwarded-Proto": proto,
				"X-Forwarded-Host":  "opengate.example.com",
			})
			prefix := injectedPrefix(body)
			assert.NotContains(t, prefix, "pwned", "proto payload reached the script")
			if strings.Contains(prefix, "OPENGATE_SERVER") {
				assert.Regexp(t, `OPENGATE_SERVER='https?://`, prefix,
					"only http/https may be emitted, got %q", prefix)
			}
		})
	}
}

// TestGetInstallScriptEmitsShellSafeQuoting pins the emitted form: a
// single-quoted POSIX word, inside which the shell expands nothing.
func TestGetInstallScriptEmitsShellSafeQuoting(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServerWithCert(t)
	body := fetchInstallScript(t, srv, "internal:8080", map[string]string{
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "opengate.example.com",
	})
	assert.Contains(t, injectedPrefix(body),
		`export OPENGATE_SERVER='https://opengate.example.com'`)
}

// TestGetInstallScriptAcceptsLegitimateHosts keeps the feature working: the
// hosts a real deployment presents must still be reflected.
func TestGetInstallScriptAcceptsLegitimateHosts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{
		"opengate.example.com",
		"opengate.example.com:8443",
		"127.0.0.1:18080",
		"localhost",
		"sub.domain.opengate-test.example",
	} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			srv, _ := newTestServerWithCert(t)
			body := fetchInstallScript(t, srv, host, nil)
			assert.Contains(t, injectedPrefix(body),
				"export OPENGATE_SERVER='https://"+host+"'")
		})
	}
}

// TestGetInstallScriptOmitsExportForUnusableHost verifies the fail-safe: when
// no trustworthy URL can be derived the server emits no OPENGATE_SERVER at all,
// leaving the script's own discovery path to run, rather than emitting a
// value an attacker chose.
func TestGetInstallScriptOmitsExportForUnusableHost(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServerWithCert(t)
	body := fetchInstallScript(t, srv, "internal:8080", map[string]string{
		"X-Forwarded-Host": "bad host$(touch /tmp/pwned)",
	})
	assert.NotContains(t, injectedPrefix(body), "OPENGATE_SERVER")
}

// TestGetInstallScriptBaseURLTakesPrecedence confirms operator configuration
// still wins over any request header.
func TestGetInstallScriptBaseURLTakesPrecedence(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServerWithCert(t)
	srv.baseURL = "https://staging.example.com"
	body := fetchInstallScript(t, srv, "internal:8080", map[string]string{
		"X-Forwarded-Host": "attacker.example.com",
	})
	prefix := injectedPrefix(body)
	assert.Contains(t, prefix, `export OPENGATE_SERVER='https://staging.example.com'`)
	assert.NotContains(t, prefix, "attacker.example.com")
}
