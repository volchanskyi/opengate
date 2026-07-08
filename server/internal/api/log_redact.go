package api

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/volchanskyi/opengate/server/internal/device"
)

const (
	// defaultLogLimit is the page size when a caller omits limit.
	defaultLogLimit = 300
	// maxLogLines caps how many lines one raw pull returns.
	maxLogLines = 1000
	// maxLogLineBytes caps the length of a single returned message so one
	// pathological line cannot blow up the response.
	maxLogLineBytes = 8192
)

// redactPlaceholder replaces any matched secret material.
const redactPlaceholder = "[REDACTED]"

// secretValueRE matches key/value secret assignments (password=…, token: …,
// api_key = …). The key is preserved; the value is stripped.
var secretValueRE = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key)(\s*[:=]\s*)(\S+)`)

// standaloneSecretRE matches secret material that carries its own recognizable
// shape regardless of surrounding key: HTTP auth headers, AWS access-key ids,
// and PEM private-key headers. It runs before secretValueRE so a `token` in an
// "Authorization: Bearer <token>" header is stripped whole rather than leaving
// the credential behind.
var standaloneSecretRE = regexp.MustCompile(`(?i)(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}|AKIA[0-9A-Z]{16}|-----BEGIN[A-Z ]*PRIVATE KEY-----`)

// clampLogLimit normalizes a caller-supplied limit into (0, maxLogLines].
func clampLogLimit(limit int) int {
	if limit <= 0 {
		return defaultLogLimit
	}
	if limit > maxLogLines {
		return maxLogLines
	}
	return limit
}

// boundLogEntries enforces the line-count and per-line byte caps on a brokered
// raw-log response — defense in depth against an agent that ignores the request
// bounds.
func boundLogEntries(entries []device.LogEntry) []device.LogEntry {
	if len(entries) > maxLogLines {
		entries = entries[:maxLogLines]
	}
	for i := range entries {
		if len(entries[i].Message) > maxLogLineBytes {
			entries[i].Message = entries[i].Message[:maxLogLineBytes]
		}
	}
	return entries
}

// redactLogEntries scrubs known secret patterns from each message in place. It
// is a server-side backstop: it runs even when agent-side redaction is disabled,
// so secrets never reach the browser.
func redactLogEntries(entries []device.LogEntry) {
	for i := range entries {
		entries[i].Message = redactSecrets(entries[i].Message)
	}
}

// redactSecrets removes secret material from a single line. Standalone patterns
// run first so credentials embedded in a key/value pair are stripped whole.
func redactSecrets(s string) string {
	s = standaloneSecretRE.ReplaceAllString(s, redactPlaceholder)
	s = secretValueRE.ReplaceAllString(s, "$1$2"+redactPlaceholder)
	return s
}

// logAuditDetails renders the requested window/filters for the audit trail
// without echoing raw log content.
func logAuditDetails(filter device.LogFilter) string {
	parts := make([]string, 0, 6)
	if filter.Level != "" {
		parts = append(parts, "level="+filter.Level)
	}
	if filter.From != "" {
		parts = append(parts, "from="+filter.From)
	}
	if filter.To != "" {
		parts = append(parts, "to="+filter.To)
	}
	if filter.Search != "" {
		parts = append(parts, fmt.Sprintf("search_len=%d", len(filter.Search)))
	}
	parts = append(parts, fmt.Sprintf("offset=%d", filter.Offset), fmt.Sprintf("limit=%d", filter.Limit))
	return strings.Join(parts, " ")
}
