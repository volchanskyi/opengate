package api

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/volchanskyi/opengate/server/internal/device"
)

const (
	defaultLogLimit = 300
	maxLogLines     = 1000
	// maxLogLineBytes caps one returned message so a single long line cannot bloat the response.
	maxLogLineBytes = 8192
)

const redactPlaceholder = "[REDACTED]"

// secretValueRE matches key/value secret assignments, keeping the key and stripping the value.
var secretValueRE = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key)(\s*[:=]\s*)(\S+)`)

// standaloneSecretRE matches self-identifying secrets; it runs before secretValueRE so a Bearer
// header is stripped whole.
var standaloneSecretRE = regexp.MustCompile(`(?i)(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{20,}|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+|[a-z][a-z0-9+.-]*://[^\s:@/]+:[^\s:@/]+@\S+|-----BEGIN[A-Z ]*PRIVATE KEY-----`)

func clampLogLimit(limit int) int {
	if limit <= 0 {
		return defaultLogLimit
	}
	if limit > maxLogLines {
		return maxLogLines
	}
	return limit
}

// boundLogEntries caps a brokered response against an agent that ignores the request bounds.
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

// redactLogEntries scrubs secrets from each message in place, even with agent-side redaction off.
func redactLogEntries(entries []device.LogEntry) {
	for i := range entries {
		entries[i].Message = redactSecrets(entries[i].Message)
	}
}

// redactSecrets removes secrets from one line; standalone patterns run first to strip whole.
func redactSecrets(s string) string {
	s = standaloneSecretRE.ReplaceAllString(s, redactPlaceholder)
	s = secretValueRE.ReplaceAllString(s, "$1$2"+redactPlaceholder)
	return s
}

// logAuditDetails renders the filters for the audit trail without the raw search term.
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
