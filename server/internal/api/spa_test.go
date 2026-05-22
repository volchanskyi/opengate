package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func newTestServerWithWebDir(t *testing.T, webDir string) *Server {
	t.Helper()
	store := testutil.NewTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewServer(ServerConfig{
		Store:    store,
		Audit:    testutil.NewTestAudit(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Groups:         testutil.NewTestGroups(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		DeviceLogs:     testutil.NewTestLogs(t, store),
		JWT:      testJWTConfig(),
		Agents:   &stubAgentGetter{},
		AMT:      &stubAMTOperator{},
		Relay:    relay.NewRelay(slog.Default()),
		Notifier: &notifications.NoopNotifier{},
		Logger:   logger,
		WebDir:   webDir,
	})
}

func TestSPA_PathTraversal_Returns404(t *testing.T) {
	t.Parallel()
	webDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>SPA</html>"), 0644))

	srv := newTestServerWithWebDir(t, webDir)

	tests := []struct {
		name string
		path string
	}{
		{"dot-dot slash", "/../../../etc/passwd"},
		{"encoded dot-dot", "/%2e%2e/%2e%2e/etc/passwd"},
		{"dot-dot in middle", "/static/../../../etc/passwd"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := doRequest(srv, http.MethodGet, tc.path, "", nil)
			// Must not serve files outside webDir; 301/404 are both acceptable rejections.
			// In particular, the response body must never contain content sourced from
			// outside webDir (e.g. /etc/passwd's "root:" prefix).
			assert.NotEqual(t, http.StatusOK, w.Code, "traversal path %s should not return 200", tc.path)
			assert.NotContains(t, w.Body.String(), "root:", "traversal path %s leaked /etc/passwd content", tc.path)
		})
	}
}

// TestSPA_SymlinkEscape_Refused covers an attack vector that the prior
// filepath.Clean+HasPrefix validation could miss: a symlink inside webDir
// pointing at a file outside webDir. os.OpenRoot rejects this because the
// resolved target escapes the root.
func TestSPA_SymlinkEscape_Refused(t *testing.T) {
	t.Parallel()
	webDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>SPA</html>"), 0644))

	// Create a symlink inside webDir that points outside (to /etc/hostname,
	// which exists on Linux runners and is harmless to read).
	outsideTarget := "/etc/hostname"
	if _, err := os.Stat(outsideTarget); err != nil {
		t.Skipf("test target %s missing on this platform: %v", outsideTarget, err)
	}
	symlinkPath := filepath.Join(webDir, "escape.txt")
	require.NoError(t, os.Symlink(outsideTarget, symlinkPath))

	srv := newTestServerWithWebDir(t, webDir)

	w := doRequest(srv, http.MethodGet, "/escape.txt", "", nil)
	// os.Root rejects symlinks resolving outside the root → fallback to SPA.
	// Either way, the response body must NOT contain /etc/hostname content.
	hostnameBytes, _ := os.ReadFile(outsideTarget)
	if len(hostnameBytes) > 0 {
		assert.NotContains(t, w.Body.String(), string(hostnameBytes),
			"symlink-escape leaked %s content", outsideTarget)
	}
}

func TestSPA_ServesStaticFile(t *testing.T) {
	t.Parallel()
	webDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>SPA</html>"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(webDir, "assets"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte("console.log('ok')"), 0644))

	srv := newTestServerWithWebDir(t, webDir)

	w := doRequest(srv, http.MethodGet, "/assets/app.js", "", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "console.log")
}

func TestSPA_FallsBackToIndex(t *testing.T) {
	t.Parallel()
	webDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>SPA</html>"), 0644))

	srv := newTestServerWithWebDir(t, webDir)

	w := doRequest(srv, http.MethodGet, "/devices", "", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "<html>SPA</html>")
}

func TestSPA_APIPathsNotIntercepted(t *testing.T) {
	t.Parallel()
	webDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>SPA</html>"), 0644))

	srv := newTestServerWithWebDir(t, webDir)

	// API and WS paths should not serve the SPA
	for _, path := range []string{"/api/v1/health", "/ws/relay/fake-token"} {
		t.Run(path, func(t *testing.T) {
			w := doRequest(srv, http.MethodGet, path, "", nil)
			// These should hit the real API handler, not the SPA fallback
			assert.NotContains(t, w.Body.String(), "<html>SPA</html>")
		})
	}
}

func TestSPA_DisabledWhenWebDirEmpty(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(ServerConfig{
		Store:    store,
		Audit:    testutil.NewTestAudit(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Groups:         testutil.NewTestGroups(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		DeviceLogs:     testutil.NewTestLogs(t, store),
		JWT:      &auth.JWTConfig{Secret: "test-secret-key-at-least-32-bytes!", Issuer: "test", Duration: 60},
		Agents:   &stubAgentGetter{},
		AMT:      &stubAMTOperator{},
		Relay:    relay.NewRelay(slog.Default()),
		Notifier: &notifications.NoopNotifier{},
		Logger:   logger,
		// WebDir deliberately empty
	})

	w := doRequest(srv, http.MethodGet, "/some-page", "", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
