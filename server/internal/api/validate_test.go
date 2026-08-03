package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInvalidText covers the shared bound applied to every user-supplied
// free-text field that the server persists, logs, or forwards to an agent.
func TestInvalidText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		value  string
		maxLen int
		wantOK bool
	}{
		{"empty is allowed", "", 128, true},
		{"ordinary name", "Ops Team — EMEA", 128, true},
		{"unicode within bound", strings.Repeat("é", 60), 128, true},
		{"exactly at the bound", strings.Repeat("a", 128), 128, true},
		{"one past the bound", strings.Repeat("a", 129), 128, false},
		{"far past the bound", strings.Repeat("a", 100000), 128, false},
		{"newline", "line one\nline two", 128, false},
		{"carriage return", "line one\rline two", 128, false},
		{"tab", "col\tcol", 128, false},
		{"null byte", "abc\x00def", 128, false},
		{"escape sequence", "abc\x1b[31mred", 128, false},
		{"delete character", "abc\x7f", 128, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := invalidText("field", tt.value, tt.maxLen)
			if tt.wantOK {
				assert.Empty(t, msg, "value should be accepted")
				return
			}
			assert.NotEmpty(t, msg, "value should be rejected")
			assert.Contains(t, msg, "field", "message should name the field")
		})
	}
}

// TestSanitizeText covers the sink-side bound used where the API contract has
// no 400 response available to reject the value with.
func TestSanitizeText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		value  string
		maxLen int
		want   string
	}{
		{"passes through clean text", "scheduled reboot", 512, "scheduled reboot"},
		{"strips newlines", "line one\nline two", 512, "line oneline two"},
		{"strips carriage returns and tabs", "a\r\tb", 512, "ab"},
		{"strips null bytes", "a\x00b", 512, "ab"},
		{"strips escape sequences", "a\x1b[31mb", 512, "a[31mb"},
		{"truncates to the bound", strings.Repeat("a", 20), 8, strings.Repeat("a", 8)},
		{"truncates by rune not byte", strings.Repeat("日", 20), 4, strings.Repeat("日", 4)},
		{"strips before truncating", "\n\n" + strings.Repeat("b", 10), 4, strings.Repeat("b", 4)},
		{"empty stays empty", "", 8, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeText(tt.value, tt.maxLen)
			assert.Equal(t, tt.want, got)
			assert.Empty(t, invalidText("field", got, tt.maxLen),
				"sanitized output must always satisfy the validator")
		})
	}
}

// TestInvalidTextBoundsByRuneCount pins that the bound counts characters, so a
// multi-byte name is not rejected for being under the limit in characters but
// over it in bytes.
func TestInvalidTextBoundsByRuneCount(t *testing.T) {
	t.Parallel()
	// 40 three-byte runes: 120 bytes, 40 characters.
	value := strings.Repeat("日", 40)
	assert.Empty(t, invalidText("name", value, 40))
	assert.NotEmpty(t, invalidText("name", value, 39))
}
