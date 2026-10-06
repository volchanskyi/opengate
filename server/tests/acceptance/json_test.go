package acceptance

import (
	"encoding/json"
	"io"
	"strings"
)

// quoteJSON renders s as a JSON string literal, escaping newlines for a request body.
func quoteJSON(s string) string {
	quoted, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(quoted)
}

func stringReader(s string) io.Reader { return strings.NewReader(s) }
