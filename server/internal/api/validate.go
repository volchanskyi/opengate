package api

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Length bounds, in characters, for user-supplied free text; nothing else enforces them.
const (
	maxDisplayNameLen = 128
	maxSiteNameLen    = 128
	maxReasonLen      = 512
	maxLabelLen       = 128
	// maxEmailLen is the RFC 5321 maximum length of a forward path.
	maxEmailLen = 254
)

// invalidText returns the reason value is refused, or "" when it is acceptable.
// Control characters are refused because the value reaches audit trails, logs and agent commands.
func invalidText(field, value string, maxLen int) string {
	if utf8.RuneCountInString(value) > maxLen {
		return fmt.Sprintf("%s must be at most %d characters", field, maxLen)
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return field + " must not contain control characters"
	}
	return ""
}

// sanitizeText drops control characters and truncates to maxLen, for endpoints with no 400.
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
