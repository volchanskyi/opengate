package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/device"
)

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantHidden string // substring that must NOT survive; "" means unchanged
		wantKept   string // substring that must survive
	}{
		{"bearer token", "Authorization: Bearer abcDEF012345_tok", "abcDEF012345_tok", "[REDACTED]"},
		{"password assignment", "db password=hunter2secret ok", "hunter2secret", "password="},
		{"api key colon", "api_key: sk-livexyz12345 done", "sk-livexyz12345", "api_key:"},
		{"aws access key", "found AKIAIOSFODNN7EXAMPLE in env", "AKIAIOSFODNN7EXAMPLE", "[REDACTED]"},
		{"pem header", "-----BEGIN RSA PRIVATE KEY----- MIIB", "PRIVATE KEY", "[REDACTED]"},
		{"benign line untouched", "user alice logged in from 10.0.0.1", "", "user alice logged in from 10.0.0.1"},
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
	got := logAuditDetails(device.LogFilter{Level: "ERROR", Search: "secret", Offset: 20, Limit: 50})
	assert.Contains(t, got, "level=ERROR")
	assert.Contains(t, got, "offset=20")
	assert.Contains(t, got, "limit=50")
	// The raw search term is never echoed — only its length.
	assert.NotContains(t, got, "search=secret")
	assert.Contains(t, got, "search_len=6")
}
