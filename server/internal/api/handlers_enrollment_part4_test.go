package api

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestEnroll(t *testing.T) {
	t.Parallel()
	t.Run("success", enrollSuccess)
	t.Run("increments use count on CSR signing", enrollIncrementsUseCount)
	t.Run("probe without CSR does not increment use count", enrollProbeDoesNotIncrementUseCount)
	t.Run("uses quicHost override for server_addr", enrollUsesQUICHostOverride)
	t.Run("invalid token", enrollInvalidToken)
	t.Run("expired token", enrollExpiredToken)
	t.Run("exhausted token", enrollExhaustedToken)
	t.Run("CSR signing failure returns 400", enrollCSRSigningFailure)
	t.Run("no cert provider", enrollNoCertProvider)
}

func enrollSuccess(t *testing.T) {
	srv, cfg := newTestServerWithCert(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)
	tok := createEnrollmentTokenViaAPI(t, srv, adminToken, CreateEnrollmentTokenRequest{})

	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: ""})
	assert.Equal(t, http.StatusOK, w.Code)

	var resp EnrollResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp.CaPem, "BEGIN CERTIFICATE")
	assert.Contains(t, resp.ServerAddr, ":9090")
	assert.NotEmpty(t, resp.ServerDomain)
	assert.Nil(t, resp.CertPem)
}

func enrollIncrementsUseCount(t *testing.T) {
	srv, cfg := newTestServerWithSigning(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)
	maxUses := 2
	tok := createEnrollmentTokenViaAPI(t, srv, adminToken, CreateEnrollmentTokenRequest{MaxUses: &maxUses})

	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: testCSRPEM})
	assert.Equal(t, http.StatusOK, w.Code)
	w = doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: testCSRPEM})
	assert.Equal(t, http.StatusOK, w.Code)
	w = doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: testCSRPEM})
	assert.Equal(t, http.StatusGone, w.Code)
}
