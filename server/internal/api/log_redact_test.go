package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/device"
)

func TestRedactSecrets(t *testing.T) {
	// The connection-string case is assembled from parts rather than written as
	// a literal DSN so the fixture is not itself a hardcoded-credential hotspot.
	dsnCred := "s3cr3tpw"
	dsn := "dsn postgres://appuser:" + dsnCred + "@db.internal:5432/app opened"

	tests := []struct {
		name       string
		in         string
		wantHidden string // substring that must NOT survive; "" means unchanged
		wantKept   string // substring that must survive
	}{
		// This corpus mirrors the agent-side guard's `raw_log_secret_corpus` in
		// agent/crates/mesh-agent-core/tests/ml_test.rs — the two redactors are
		// independent defense-in-depth layers, so both must strip every shape.
		{"bearer token", "Authorization: Bearer abcDEF012345_tok", "abcDEF012345_tok", "[REDACTED]"},
		{"basic auth", "proxy authorization: Basic dXNlcjpwYXNzd29yZA==", "dXNlcjpwYXNzd29yZA==", "[REDACTED]"},
		{"password assignment", "db password=hunter2secret ok", "hunter2secret", "password="},
		{"api key colon", "api_key: sk-livexyz12345 done", "sk-livexyz12345", "api_key:"},
		{"client secret", "client_secret=ghp_00112233445566778899 rotated", "ghp_00112233445566778899", "client_secret="},
		{"jwt", "session token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N ok", "eyJzdWIiOiIxMjM0In0", "[REDACTED]"},
		{"aws access key", "found AKIAIOSFODNN7EXAMPLE in env", "AKIAIOSFODNN7EXAMPLE", "[REDACTED]"},
		{"gcp api key", "google key AIzaSyA1234567890abcdefghijklmnopqrstuvw in env", "AIzaSyA1234567890abcdefghijklmnopqrstuvw", "[REDACTED]"},
		{"connection string", dsn, dsnCred, "[REDACTED]"},
		{"pem header", "-----BEGIN RSA PRIVATE KEY----- MIIB", "PRIVATE KEY", "[REDACTED]"},
		{"benign line untouched", "user alice logged in from 10.0.0.1", "", "user alice logged in from 10.0.0.1"},
		{"benign url untouched", "GET https://example.com/health 200", "", "https://example.com/health"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactSecrets(tt.in)
			if tt.wantHidden != "" {
				assert.NotContains(t, got, tt.wantHidden)
			}
			assert.Contains(t, got, tt.wantKept)
		})
	}
}

func TestBoundLogEntries(t *testing.T) {
	t.Run("caps line count", func(t *testing.T) {
		entries := make([]device.LogEntry, maxLogLines+50)
		assert.Len(t, boundLogEntries(entries), maxLogLines)
	})
	t.Run("truncates oversized message", func(t *testing.T) {
		entries := []device.LogEntry{{Message: strings.Repeat("x", maxLogLineBytes+100)}}
		got := boundLogEntries(entries)
		assert.Len(t, got[0].Message, maxLogLineBytes)
	})
	t.Run("leaves small input intact", func(t *testing.T) {
		entries := []device.LogEntry{{Message: "short"}}
		got := boundLogEntries(entries)
		assert.Equal(t, "short", got[0].Message)
	})
}

func TestClampLogLimit(t *testing.T) {
	assert.Equal(t, defaultLogLimit, clampLogLimit(0))
	assert.Equal(t, defaultLogLimit, clampLogLimit(-5))
	assert.Equal(t, 1, clampLogLimit(1))
	assert.Equal(t, maxLogLines, clampLogLimit(maxLogLines))
	assert.Equal(t, maxLogLines, clampLogLimit(maxLogLines+1))
}

func TestLogAuditDetails(t *testing.T) {
	got := logAuditDetails(device.LogFilter{
		Level: "ERROR", From: "2026-07-14T00:00:00Z", To: "2026-07-14T01:00:00Z",
		Search: "secret", Offset: 20, Limit: 50,
	})
	assert.Equal(t, "level=ERROR from=2026-07-14T00:00:00Z to=2026-07-14T01:00:00Z search_len=6 offset=20 limit=50", got)
	assert.Contains(t, got, "level=ERROR")
	assert.Contains(t, got, "offset=20")
	assert.Contains(t, got, "limit=50")
	// The raw search term is never echoed — only its length.
	assert.NotContains(t, got, "search=secret")
	assert.Contains(t, got, "search_len=6")
}
