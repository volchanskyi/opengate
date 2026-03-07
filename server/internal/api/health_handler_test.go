package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/relay"
)

func TestHealthHandler(t *testing.T) {
	srv, _ := newTestServer(t)

	t.Run("returns ok when database is reachable", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/health", "", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]string
		json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "ok", resp["status"])
	})

	t.Run("returns 503 when database is unreachable", func(t *testing.T) {
		dir := t.TempDir()
		store, err := db.NewSQLiteStore(filepath.Join(dir, "health.db"))
		require.NoError(t, err)
		cfg := &auth.JWTConfig{Secret: "test-secret-key-at-least-32-bytes!", Issuer: "test", Duration: 15 * time.Minute}
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		closedSrv := NewServer(store, cfg, &stubAgentGetter{}, relay.NewRelay(), logger)
		store.Close()

		w := doRequest(closedSrv, http.MethodGet, "/api/v1/health", "", nil)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}
