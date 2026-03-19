// Package wsman implements WSMAN (Web Services Management) over APF channels
// for communicating with Intel AMT devices via the CIRA tunnel.
package wsman

import (
	"crypto/md5"  //nolint:gosec // Digest auth requires MD5 per RFC 2617
	"crypto/rand" //nolint:gosec
	"fmt"
	"strings"
)

// DigestAuth handles HTTP Digest authentication per RFC 2617.
type DigestAuth struct {
	Username string
	Password string
}

// Authorize parses a WWW-Authenticate header and produces an Authorization header value.
func (d *DigestAuth) Authorize(method, uri, wwwAuth string) (string, error) {
	params, err := parseChallenge(wwwAuth)
	if err != nil {
		return "", err
	}

	realm := params["realm"]
	nonce := params["nonce"]
	qop := params["qop"]

	cnonce := randomHex(8)
	nc := "00000001"

	ha1 := md5Hash(d.Username + ":" + realm + ":" + d.Password)
	ha2 := md5Hash(method + ":" + uri)
	response := computeResponse(ha1, nonce, nc, cnonce, qop, ha2)

	return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", `+
		`qop=%s, nc=%s, cnonce="%s", response="%s"`,
		d.Username, realm, nonce, uri, qop, nc, cnonce, response), nil
}

// parseChallenge extracts parameters from a WWW-Authenticate: Digest header.
func parseChallenge(header string) (map[string]string, error) {
	if !strings.HasPrefix(header, "Digest ") {
		return nil, fmt.Errorf("wsman: expected Digest auth, got %q", header)
	}
	params := make(map[string]string)
	parts := strings.Split(header[7:], ",")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		val = strings.Trim(val, `"`)
		params[key] = val
	}
	return params, nil
}

// computeResponse calculates the Digest response value per RFC 2617.
func computeResponse(ha1, nonce, nc, cnonce, qop, ha2 string) string {
	return md5Hash(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
}

// md5Hash returns the hex-encoded MD5 hash of s.
// MD5 is mandated by the HTTP Digest Authentication protocol (RFC 2617 / RFC 7616).
// This is not used for password storage or any other security-sensitive purpose.
func md5Hash(s string) string {
	h := md5.Sum([]byte(s)) //nolint:gosec // MD5 required by HTTP Digest Auth spec (RFC 7616)
	return fmt.Sprintf("%x", h)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return fmt.Sprintf("%x", b)
}
