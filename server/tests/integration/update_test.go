package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/api"
	"net/http"
	"testing"
)

// publishManifest publishes an update manifest via the REST API.
func publishManifest(t *testing.T, env *sessionTestEnv, jwt, version, osName, arch string) api.AgentManifest {
	t.Helper()

	hash := sha256.Sum256([]byte("fake-binary-" + version))
	body := api.PublishUpdateRequest{
		Version: version,
		Os:      osName,
		Arch:    arch,
		Url:     "https://example.com/agent-" + version,
		Sha256:  hex.EncodeToString(hash[:]),
	}

	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))

	req, err := http.NewRequest(http.MethodPost, env.httpSrv.URL+"/api/v1/updates/manifests", &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", bearerPrefix+jwt)

	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var manifest api.AgentManifest
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&manifest))
	assert.Equal(t, version, manifest.Version)
	assert.NotEmpty(t, manifest.Signature, "manifest should be signed")
	return manifest
}

// pushUpdate pushes an update to connected agents via the REST API.
func pushUpdate(t *testing.T, env *sessionTestEnv, jwt, version, osName, arch string) api.PushUpdateResponse {
	t.Helper()

	body := api.PushUpdateRequest{
		Version: version,
		Os:      osName,
		Arch:    arch,
	}

	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))

	req, err := http.NewRequest(http.MethodPost, env.httpSrv.URL+"/api/v1/updates/push", &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", bearerPrefix+jwt)

	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result api.PushUpdateResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	return result
}
