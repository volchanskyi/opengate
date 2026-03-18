package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// stubCertProvider is a test double for CertProvider.
type stubCertProvider struct {
	pem    []byte
	signFn func(csrDER []byte) ([]byte, error)
}

func (s *stubCertProvider) CACertPEM() []byte { return s.pem }
func (s *stubCertProvider) SignAgentCSR(csrDER []byte) ([]byte, error) {
	if s.signFn != nil {
		return s.signFn(csrDER)
	}
	return nil, fmt.Errorf("SignAgentCSR not configured")
}

func newTestServerWithCert(t *testing.T) (*Server, *auth.JWTConfig) {
	t.Helper()
	store := testutil.NewTestStore(t)
	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(ServerConfig{
		Store:    store,
		JWT:      cfg,
		Agents:   &stubAgentGetter{},
		AMT:      &stubAMTOperator{},
		Cert:     &stubCertProvider{pem: []byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n")},
		Relay:    relay.NewRelay(),
		Notifier: &notifications.NoopNotifier{},
		Logger:   logger,
	})
	return srv, cfg
}

func TestCreateEnrollmentToken(t *testing.T) {
	t.Run("admin success", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		label := "test-token"
		maxUses := 5
		expiresInHours := 48
		body := CreateEnrollmentTokenRequest{
			Label:          &label,
			MaxUses:        &maxUses,
			ExpiresInHours: &expiresInHours,
		}

		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
		assert.Equal(t, http.StatusCreated, w.Code)

		var tok EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))
		assert.Equal(t, "test-token", tok.Label)
		assert.Equal(t, 5, tok.MaxUses)
		assert.Equal(t, 0, tok.UseCount)
		assert.NotEmpty(t, tok.Token)
		assert.Len(t, tok.Token, 64) // 32 bytes hex
	})

	t.Run("non-admin forbidden", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

		body := CreateEnrollmentTokenRequest{}
		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", userToken, body)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)

		body := CreateEnrollmentTokenRequest{}
		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestListEnrollmentTokens(t *testing.T) {
	t.Run("returns created tokens", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		// Create two tokens.
		for _, label := range []string{"token-1", "token-2"} {
			l := label
			body := CreateEnrollmentTokenRequest{Label: &l}
			w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
			require.Equal(t, http.StatusCreated, w.Code)
		}

		w := doRequest(srv, http.MethodGet, "/api/v1/enrollment-tokens", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var tokens []EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
		assert.Len(t, tokens, 2)
	})

	t.Run("empty list", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		w := doRequest(srv, http.MethodGet, "/api/v1/enrollment-tokens", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var tokens []EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
		assert.Empty(t, tokens)
	})

	t.Run("non-admin forbidden", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

		w := doRequest(srv, http.MethodGet, "/api/v1/enrollment-tokens", userToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestDeleteEnrollmentToken(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		// Create a token.
		body := CreateEnrollmentTokenRequest{}
		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
		require.Equal(t, http.StatusCreated, w.Code)

		var tok EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))

		// Delete it.
		w = doRequest(srv, http.MethodDelete, "/api/v1/enrollment-tokens/"+tok.Id.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Verify it's gone.
		w = doRequest(srv, http.MethodGet, "/api/v1/enrollment-tokens", adminToken, nil)
		require.Equal(t, http.StatusOK, w.Code)

		var tokens []EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
		assert.Empty(t, tokens)
	})

	t.Run("not found", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		w := doRequest(srv, http.MethodDelete, "/api/v1/enrollment-tokens/00000000-0000-0000-0000-000000000000", adminToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("non-admin forbidden", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

		w := doRequest(srv, http.MethodDelete, "/api/v1/enrollment-tokens/00000000-0000-0000-0000-000000000000", userToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestEnroll(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		// Create an enrollment token.
		body := CreateEnrollmentTokenRequest{}
		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
		require.Equal(t, http.StatusCreated, w.Code)

		var tok EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))

		// Enroll (public endpoint, no auth).
		enrollBody := EnrollRequest{CsrPem: ""}
		w = doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", enrollBody)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp EnrollResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.CaPem, "BEGIN CERTIFICATE")
		assert.Contains(t, resp.ServerAddr, ":9090")
		assert.NotEmpty(t, resp.ServerDomain)
		assert.Nil(t, resp.CertPem) // no CSR submitted
	})

	t.Run("increments use count", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		maxUses := 2
		body := CreateEnrollmentTokenRequest{MaxUses: &maxUses}
		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
		require.Equal(t, http.StatusCreated, w.Code)

		var tok EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))

		// First enroll.
		w = doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{})
		assert.Equal(t, http.StatusOK, w.Code)

		// Second enroll.
		w = doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{})
		assert.Equal(t, http.StatusOK, w.Code)

		// Third should fail — exhausted.
		w = doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{})
		assert.Equal(t, http.StatusGone, w.Code)
	})

	t.Run("uses quicHost override for server_addr", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		srv.quicHost = "quic.opengate.example.com"
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		body := CreateEnrollmentTokenRequest{}
		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
		require.Equal(t, http.StatusCreated, w.Code)

		var tok EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))

		w = doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{})
		assert.Equal(t, http.StatusOK, w.Code)

		var resp EnrollResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "quic.opengate.example.com:9090", resp.ServerAddr)
	})

	t.Run("invalid token", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)

		w := doRequest(srv, http.MethodPost, "/api/v1/enroll/nonexistent-token", "", EnrollRequest{})
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("expired token", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		user, _ := seedTestUser(t, srv, cfg, "admin@test.com", true)

		// Insert an already-expired token directly into the store.
		et := &db.EnrollmentToken{
			ID:        uuid.New(),
			Token:     "expired-token-abc123",
			Label:     "expired",
			CreatedBy: user.ID,
			MaxUses:   0,
			UseCount:  0,
			ExpiresAt: time.Now().UTC().Add(-1 * time.Hour),
		}
		err := srv.store.CreateEnrollmentToken(t.Context(), et)
		require.NoError(t, err)

		w := doRequest(srv, http.MethodPost, "/api/v1/enroll/expired-token-abc123", "", EnrollRequest{})
		assert.Equal(t, http.StatusGone, w.Code)
	})

	t.Run("exhausted token", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		user, _ := seedTestUser(t, srv, cfg, "admin@test.com", true)

		// Insert token with max_uses=1, use_count=1 directly.
		et := &db.EnrollmentToken{
			ID:        uuid.New(),
			Token:     "exhausted-token-abc123",
			Label:     "exhausted",
			CreatedBy: user.ID,
			MaxUses:   1,
			UseCount:  1,
			ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		}
		err := srv.store.CreateEnrollmentToken(t.Context(), et)
		require.NoError(t, err)

		w := doRequest(srv, http.MethodPost, "/api/v1/enroll/exhausted-token-abc123", "", EnrollRequest{})
		assert.Equal(t, http.StatusGone, w.Code)
	})

	t.Run("no cert provider", func(t *testing.T) {
		// Server without cert provider should fail enrollment.
		srv, cfg := newTestServer(t)
		user, _ := seedTestUser(t, srv, cfg, "admin@test.com", true)

		et := &db.EnrollmentToken{
			ID:        uuid.New(),
			Token:     "token-no-cert",
			Label:     "no-cert",
			CreatedBy: user.ID,
			MaxUses:   0,
			UseCount:  0,
			ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		}
		err := srv.store.CreateEnrollmentToken(t.Context(), et)
		require.NoError(t, err)

		w := doRequest(srv, http.MethodPost, "/api/v1/enroll/token-no-cert", "", EnrollRequest{})
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestGetServerCA(t *testing.T) {
	t.Run("authenticated success", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

		w := doRequest(srv, http.MethodGet, "/api/v1/server/ca", userToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp CACertResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.Pem, "BEGIN CERTIFICATE")
	})

	t.Run("unauthenticated", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)

		w := doRequest(srv, http.MethodGet, "/api/v1/server/ca", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestGetInstallScript(t *testing.T) {
	t.Run("returns script", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)

		w := doRequest(srv, http.MethodGet, "/api/v1/server/install.sh", "", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "#!/usr/bin/env bash")
		assert.Contains(t, w.Body.String(), "OpenGate Agent Installer")
	})

	t.Run("injects server URL from Host header", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/server/install.sh", nil)
		req.Host = "opengate.example.com"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `export OPENGATE_SERVER="https://opengate.example.com"`)
	})

	t.Run("uses BaseURL config when set", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)
		srv.baseURL = "https://staging.example.com"

		req := httptest.NewRequest(http.MethodGet, "/api/v1/server/install.sh", nil)
		req.Host = "127.0.0.1:18080"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `export OPENGATE_SERVER="https://staging.example.com"`)
		assert.NotContains(t, w.Body.String(), "127.0.0.1")
	})

	t.Run("uses X-Forwarded headers when present", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/server/install.sh", nil)
		req.Host = "internal:8080"
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "opengate.cloudisland.net")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `export OPENGATE_SERVER="https://opengate.cloudisland.net"`)
	})

	t.Run("injects OPENGATE_GITHUB_REPO when configured", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)
		srv.githubRepo = "volchanskyi/opengate"

		req := httptest.NewRequest(http.MethodGet, "/api/v1/server/install.sh", nil)
		req.Host = "opengate.example.com"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `export OPENGATE_GITHUB_REPO="volchanskyi/opengate"`)
		assert.Contains(t, w.Body.String(), "# Injected by server")
	})

	t.Run("omits OPENGATE_GITHUB_REPO when not configured", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/server/install.sh", nil)
		req.Host = "opengate.example.com"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotContains(t, w.Body.String(), `export OPENGATE_GITHUB_REPO=`)
	})
}
