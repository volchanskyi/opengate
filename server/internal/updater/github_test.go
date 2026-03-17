package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSigningKeys(t *testing.T) (*SigningKeys, string) {
	t.Helper()
	dir := t.TempDir()
	keys, err := LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)
	return keys, dir
}

// newFakeGitHub creates a test server that serves a GitHub-like releases API.
// The sha256URLs map is keyed by asset name suffix (e.g., "linux-amd64").
func newFakeGitHub(t *testing.T, tag string, binaries map[string]string) *httptest.Server {
	t.Helper()

	// We need the server URL in the response, so use a pointer we fill after creation.
	var srvURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/releases/latest":
			assets := ""
			i := 0
			for suffix, sha256 := range binaries {
				if i > 0 {
					assets += ","
				}
				name := "mesh-agent-" + suffix
				assets += fmt.Sprintf(
					`{"name":%q,"browser_download_url":"https://releases.example.com/%s"},`+
						`{"name":"%s.sha256","browser_download_url":"%s/sha256/%s"}`,
					name, name, name, srvURL, suffix,
				)
				_ = sha256
				i++
			}
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[%s]}`, tag, assets)

		default:
			// Serve SHA256 files at /sha256/{suffix}.
			for suffix, sha256 := range binaries {
				if r.URL.Path == "/sha256/"+suffix {
					fmt.Fprintf(w, "%s  mesh-agent-%s\n", sha256, suffix)
					return
				}
			}
			http.NotFound(w, r)
		}
	}))
	srvURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

func TestSyncFromGitHub_Success(t *testing.T) {
	keys, dataDir := setupSigningKeys(t)
	store := NewManifestStore(dataDir)

	sha256Amd64 := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	sha256Arm64 := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	srv := newFakeGitHub(t, "v0.1.1", map[string]string{
		"linux-amd64": sha256Amd64,
		"linux-arm64": sha256Arm64,
	})

	synced, err := SyncFromGitHub(context.Background(), "owner/repo", srv.URL, keys, store)
	require.NoError(t, err)
	assert.Len(t, synced, 2)

	// Verify amd64 manifest.
	m, err := store.Get(context.Background(), "linux", "amd64")
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "0.1.1", m.Version)
	assert.Equal(t, "linux", m.OS)
	assert.Equal(t, "amd64", m.Arch)
	assert.Equal(t, "https://releases.example.com/mesh-agent-linux-amd64", m.URL)
	assert.Equal(t, sha256Amd64, m.SHA256)
	assert.NotEmpty(t, m.Signature)

	// Verify arm64 manifest.
	m2, err := store.Get(context.Background(), "linux", "arm64")
	require.NoError(t, err)
	require.NotNil(t, m2)
	assert.Equal(t, "0.1.1", m2.Version)
	assert.Equal(t, "arm64", m2.Arch)
	assert.Equal(t, sha256Arm64, m2.SHA256)

	// Verify signatures are valid.
	valid, err := keys.VerifyHash(m.SHA256, m.Signature)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestSyncFromGitHub_NoMatchingAssets(t *testing.T) {
	keys, dataDir := setupSigningKeys(t)
	store := NewManifestStore(dataDir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v1.0.0","assets":[]}`)
	}))
	t.Cleanup(srv.Close)

	synced, err := SyncFromGitHub(context.Background(), "owner/repo", srv.URL, keys, store)
	require.NoError(t, err)
	assert.Empty(t, synced)
}

func TestSyncFromGitHub_APIError(t *testing.T) {
	keys, dataDir := setupSigningKeys(t)
	store := NewManifestStore(dataDir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := SyncFromGitHub(context.Background(), "owner/repo", srv.URL, keys, store)
	assert.Error(t, err)
}

func TestSyncFromGitHub_EmptyRepo(t *testing.T) {
	keys, dataDir := setupSigningKeys(t)
	store := NewManifestStore(dataDir)

	_, err := SyncFromGitHub(context.Background(), "", "https://api.github.com", keys, store)
	assert.Error(t, err)
}

func TestSyncFromGitHub_MalformedSHA256(t *testing.T) {
	keys, dataDir := setupSigningKeys(t)
	store := NewManifestStore(dataDir)

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.1.0","assets":[
				{"name":"mesh-agent-linux-amd64","browser_download_url":"https://example.com/binary"},
				{"name":"mesh-agent-linux-amd64.sha256","browser_download_url":"%s/bad-sha256"}
			]}`, srvURL)
		case "/bad-sha256":
			fmt.Fprint(w, "not-a-valid-hex!!!")
		}
	}))
	srvURL = srv.URL
	t.Cleanup(srv.Close)

	_, err := SyncFromGitHub(context.Background(), "owner/repo", srv.URL, keys, store)
	assert.Error(t, err)
}
