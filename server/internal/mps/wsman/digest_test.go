package wsman

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseChallenge(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   map[string]string
	}{
		{
			name:   "standard AMT response",
			header: `Digest realm="Digest:A4070000000000000000000000000000", nonce="dcd98b", qop="auth"`,
			want: map[string]string{
				"realm": "Digest:A4070000000000000000000000000000",
				"nonce": "dcd98b",
				"qop":   "auth",
			},
		},
		{
			name:   "with algorithm",
			header: `Digest realm="test", nonce="abc123", qop="auth", algorithm=MD5`,
			want: map[string]string{
				"realm":     "test",
				"nonce":     "abc123",
				"qop":       "auth",
				"algorithm": "MD5",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChallenge(tt.header)
			require.NoError(t, err)
			for k, v := range tt.want {
				assert.Equal(t, v, got[k], "key %q", k)
			}
		})
	}
}

func TestParseChallengeMissingDigestPrefix(t *testing.T) {
	_, err := parseChallenge("Basic realm=\"test\"")
	assert.Error(t, err)
}

func TestComputeResponseRFC2617(t *testing.T) {
	// Test vector from RFC 2617 section 3.5.
	ha1 := md5Hash("Mufasa:testrealm@host.com:Circle Of Life")
	ha2 := md5Hash("GET:/dir/index.html")
	expected := md5Hash(ha1 + ":dcd98b7102dd2f0e8b11d0f600bfb0c093:" +
		"00000001:0a4f113b:auth:" + ha2)

	got := computeResponse(ha1, "dcd98b7102dd2f0e8b11d0f600bfb0c093",
		"00000001", "0a4f113b", "auth", ha2)
	assert.Equal(t, expected, got)
}

func TestAuthorize(t *testing.T) {
	da := &DigestAuth{Username: "admin", Password: "P@ssw0rd"}
	header := `Digest realm="Digest:A4070000", nonce="abc123", qop="auth"`
	authz, err := da.Authorize("POST", "/wsman", header)
	require.NoError(t, err)
	assert.Contains(t, authz, "Digest ")
	assert.Contains(t, authz, `username="admin"`)
	assert.Contains(t, authz, `realm="Digest:A4070000"`)
	assert.Contains(t, authz, `nonce="abc123"`)
	assert.Contains(t, authz, `qop=auth`)
	assert.Contains(t, authz, "response=")
}
