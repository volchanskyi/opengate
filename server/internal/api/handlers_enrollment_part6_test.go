package api

import (
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetInstallScriptReturnsScript(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServerWithCert(t)
	w := doRequest(srv, http.MethodGet, "/api/v1/server/install.sh", "", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "#!/usr/bin/env bash")
	assert.Contains(t, w.Body.String(), "OpenGate Agent Installer")
}

func TestGetInstallScriptUsesHostHeader(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServerWithCert(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/install.sh", nil)
	req.Host = "opengate.example.com"
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `export OPENGATE_SERVER="https://opengate.example.com"`)
}
