package api

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func enrollProbeDoesNotIncrementUseCount(t *testing.T) {
	srv, cfg := newTestServerWithSigning(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)
	maxUses := 1
	tok := createEnrollmentTokenViaAPI(t, srv, adminToken, CreateEnrollmentTokenRequest{MaxUses: &maxUses})

	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: ""})
	assert.Equal(t, http.StatusOK, w.Code)
	var resp EnrollResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Nil(t, resp.CertPem)

	w = doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: testCSRPEM})
	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotNil(t, resp.CertPem)

	w = doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: testCSRPEM})
	assert.Equal(t, http.StatusGone, w.Code)
}

func enrollUsesQUICHostOverride(t *testing.T) {
	srv, cfg := newTestServerWithCert(t)
	srv.quicHost = "quic.opengate.example.com"
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)
	tok := createEnrollmentTokenViaAPI(t, srv, adminToken, CreateEnrollmentTokenRequest{})

	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{})
	assert.Equal(t, http.StatusOK, w.Code)
	var resp EnrollResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "quic.opengate.example.com:9090", resp.ServerAddr)
}

func enrollInvalidToken(t *testing.T) {
	srv, _ := newTestServerWithCert(t)
	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/nonexistent-token", "", EnrollRequest{})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func enrollExpiredToken(t *testing.T) {
	srv, cfg := newTestServerWithCert(t)
	user, _ := seedTestUser(t, srv, cfg, "admin@test.com", true)
	createEnrollmentTokenDirect(t, srv, user.ID, "expired-token-abc123", 0, 0, -time.Hour)

	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/expired-token-abc123", "", EnrollRequest{})
	assert.Equal(t, http.StatusGone, w.Code)
}
