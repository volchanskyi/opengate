package acceptance

import (
	"encoding/json"
	"io"
	"strings"
)

// quoteJSON renders s as a JSON string literal, so a PEM block with its
// newlines survives being placed inside a request body.
func quoteJSON(s string) string {
	quoted, err := json.Marshal(s)
	if err != nil {
		// json.Marshal of a string cannot fail; the branch exists so the
		// helper has no error to hand its callers.
		return `""`
	}
	return string(quoted)
}

// stringReader adapts a body built as a string to the reader http wants.
func stringReader(s string) io.Reader { return strings.NewReader(s) }
