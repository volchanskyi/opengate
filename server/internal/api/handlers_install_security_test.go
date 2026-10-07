package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// injectedPrefix returns the block before the script's own shebang, since the embedded script
// legitimately contains the shell metacharacters an injection would add.
func injectedPrefix(body string) string {
	prefix, _, found := strings.Cut(body, "#!/usr/bin/env bash")
	if !found {
		return body
	}
	return prefix
}

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

func fetchInstallPrefix(t *testing.T, host string, headers map[string]string) string {
	t.Helper()
	srv, _ := newTestServerWithCert(t)
	return injectedPrefix(fetchInstallScript(t, srv, host, headers))
}

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
			prefix := fetchInstallPrefix(t, "internal:8080", map[string]string{"X-Forwarded-Host": tc.host})
			assert.NotContains(t, prefix, payload,
				"X-Forwarded-Host payload reached the emitted script prefix: %q", prefix)
		})
	}
}

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
			prefix := fetchInstallPrefix(t, "internal:8080", map[string]string{
				"X-Forwarded-Proto": proto,
				"X-Forwarded-Host":  "opengate.example.com",
			})
			assert.NotContains(t, prefix, "pwned", "proto payload reached the script")
			if strings.Contains(prefix, "OPENGATE_SERVER") {
				assert.Regexp(t, `OPENGATE_SERVER='https?://`, prefix,
					"only http/https may be emitted, got %q", prefix)
			}
		})
	}
}

func TestGetInstallScriptEmitsShellSafeQuoting(t *testing.T) {
	t.Parallel()
	prefix := fetchInstallPrefix(t, "internal:8080", map[string]string{
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "opengate.example.com",
	})
	assert.Contains(t, prefix, `export OPENGATE_SERVER='https://opengate.example.com'`)
}

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
			assert.Contains(t, fetchInstallPrefix(t, host, nil),
				"export OPENGATE_SERVER='https://"+host+"'")
		})
	}
}

func TestGetInstallScriptOmitsExportForUnusableHost(t *testing.T) {
	t.Parallel()
	prefix := fetchInstallPrefix(t, "internal:8080", map[string]string{
		"X-Forwarded-Host": "bad host$(touch /tmp/pwned)",
	})
	assert.NotContains(t, prefix, "OPENGATE_SERVER")
}

func TestGetInstallScriptBaseURLTakesPrecedence(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServerWithCert(t)
	srv.baseURL = "https://staging.example.com"
	prefix := injectedPrefix(fetchInstallScript(t, srv, "internal:8080", map[string]string{
		"X-Forwarded-Host": "attacker.example.com",
	}))
	assert.Contains(t, prefix, `export OPENGATE_SERVER='https://staging.example.com'`)
	assert.NotContains(t, prefix, "attacker.example.com")
}
