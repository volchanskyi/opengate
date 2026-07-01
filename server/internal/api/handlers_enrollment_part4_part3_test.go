package api

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/updater"
	"net/http"
	"testing"
	"time"
)

func enrollExhaustedToken(t *testing.T) {
	srv, cfg := newTestServerWithCert(t)
	user, _ := seedTestUser(t, srv, cfg, "admin@test.com", true)
	createEnrollmentTokenDirect(t, srv, user.ID, "exhausted-token-abc123", 1, 1, 24*time.Hour)

	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/exhausted-token-abc123", "", EnrollRequest{})
	assert.Equal(t, http.StatusGone, w.Code)
}

func enrollCSRSigningFailure(t *testing.T) {
	srv, cfg := newTestServerWithCert(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)
	tok := createEnrollmentTokenViaAPI(t, srv, adminToken, CreateEnrollmentTokenRequest{})

	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: testCSRPEM})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func enrollNoCertProvider(t *testing.T) {
	srv, cfg := newTestServer(t)
	user, _ := seedTestUser(t, srv, cfg, "admin@test.com", true)
	createEnrollmentTokenDirect(t, srv, user.ID, "token-no-cert", 0, 0, 24*time.Hour)

	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/token-no-cert", "", EnrollRequest{})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func createEnrollmentTokenViaAPI(t *testing.T, srv *Server, adminToken string, body CreateEnrollmentTokenRequest) EnrollmentToken {
	t.Helper()
	w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
	require.Equal(t, http.StatusCreated, w.Code)
	var tok EnrollmentToken
	require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))
	return tok
}

func createEnrollmentTokenDirect(t *testing.T, srv *Server, userID uuid.UUID, token string, maxUses, useCount int, ttl time.Duration) {
	t.Helper()
	err := srv.enrollment.Create(testTenantContext(t), &updater.EnrollmentToken{
		ID:        uuid.New(),
		Token:     token,
		Label:     token,
		CreatedBy: userID,
		MaxUses:   maxUses,
		UseCount:  useCount,
		ExpiresAt: time.Now().UTC().Add(ttl),
	})
	require.NoError(t, err)
}
