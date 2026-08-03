package api

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Bounds for the user-supplied free-text fields the server persists, writes to
// the audit trail, or forwards to an agent. The OpenAPI schema documents the
// shape of a request, but nothing validates it at runtime, so these are the
// enforcement point.
const (
	maxDisplayNameLen = 128
	maxGroupNameLen   = 128
	maxReasonLen      = 512
	maxLabelLen       = 128
	// maxEmailLen is the RFC 5321 maximum length of a forward path.
	maxEmailLen = 254
)

// invalidText reports why value is unacceptable as a stored free-text field, or
// "" when it is acceptable.
//
// The length bound is in characters rather than bytes so a multi-byte name is
// judged the way a reader sees it. Control characters are refused outright:
// these values reach the audit trail, structured logs, and an agent's command
// context, and none of those are places where an embedded newline, escape
// sequence, or NUL is ever meaningful.
func invalidText(field, value string, maxLen int) string {
	if utf8.RuneCountInString(value) > maxLen {
		return fmt.Sprintf("%s must be at most %d characters", field, maxLen)
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return field + " must not contain control characters"
	}
	return ""
}

// sanitizeText bounds a free-text value the same way invalidText judges one, for
// the endpoints whose API contract has no 400 response to reject it with. It
// drops control characters and truncates to maxLen characters.
func sanitizeText(value string, maxLen int) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	if utf8.RuneCountInString(cleaned) <= maxLen {
		return cleaned
	}
	return string([]rune(cleaned)[:maxLen])
}
