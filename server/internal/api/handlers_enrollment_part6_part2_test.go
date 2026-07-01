package api

import (
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetInstallScriptUsesBaseURL(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServerWithCert(t)
	srv.baseURL = "https://staging.example.com"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/install.sh", nil)
	req.Host = "127.0.0.1:18080"
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `export OPENGATE_SERVER="https://staging.example.com"`)
	assert.NotContains(t, w.Body.String(), "127.0.0.1")
}

func TestGetInstallScriptUsesForwardedHeaders(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServerWithCert(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/install.sh", nil)
	req.Host = "internal:8080"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "opengate.cloudisland.net")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `export OPENGATE_SERVER="https://opengate.cloudisland.net"`)
}
