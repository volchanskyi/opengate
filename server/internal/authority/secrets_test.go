package authority

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiServer stands in for the Kubernetes API's secrets in one namespace.
type apiServer struct {
	mu         sync.Mutex
	secrets    map[string]secretObject
	version    int
	tokens     []string
	patchTypes []string
	// drop is a key the store loses on every write, as a store that truncates would.
	drop string
}

func (a *apiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokens = append(a.tokens, r.Header.Get("Authorization"))
	const prefix = "/api/v1/namespaces/opengate/secrets"
	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, prefix), "/")
	switch r.Method {
	case http.MethodGet:
		obj, ok := a.secrets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(obj)
	case http.MethodPost, http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		var obj secretObject
		_ = json.Unmarshal(body, &obj)
		if r.Method == http.MethodPut && a.secrets[name].Metadata.ResourceVersion != obj.Metadata.ResourceVersion {
			w.WriteHeader(http.StatusConflict)
			return
		}
		a.version++
		obj.Metadata.ResourceVersion = string(rune('0' + a.version))
		delete(obj.Data, a.drop)
		a.secrets[obj.Metadata.Name] = obj
		w.WriteHeader(map[string]int{http.MethodPost: http.StatusCreated, http.MethodPut: http.StatusOK}[r.Method])
	case http.MethodPatch:
		a.patchTypes = append(a.patchTypes, r.Header.Get("Content-Type"))
		obj, ok := a.secrets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var patch struct {
			Data map[string]*[]byte `json:"data"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &patch)
		for key, value := range patch.Data {
			if value == nil {
				delete(obj.Data, key)
			} else {
				obj.Data[key] = *value
			}
		}
		a.secrets[name] = obj
		_ = json.NewEncoder(w).Encode(obj)
	}
}

func TestWriteRefusesASecretThatReadsBackDifferently(t *testing.T) {
	api := &apiServer{secrets: map[string]secretObject{}, drop: "ca.key"}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	err := NewSecrets(srv.URL, "opengate", "t", srv.Client()).Write(context.Background(), "opengate-authority", steady(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ca.key")
	assert.NotContains(t, err.Error(), "PRIVATE KEY")
}

func TestRemoveKeysDeletesOnlyTheNamedKeys(t *testing.T) {
	api := &apiServer{secrets: map[string]secretObject{
		"opengate-secrets": {Metadata: secretMetadata{Name: "opengate-secrets"}, Data: map[string][]byte{
			"ca.crt": []byte("c"), "ca.key": []byte("k"), "JWT_SECRET": []byte("j"),
		}},
	}}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	s := NewSecrets(srv.URL, "opengate", "t", srv.Client())

	require.NoError(t, s.RemoveKeys(context.Background(), "opengate-secrets", "ca.crt", "ca.key"))
	got, _, err := s.Read(context.Background(), "opengate-secrets")
	require.NoError(t, err)
	assert.Equal(t, []string{"JWT_SECRET"}, got.Names())
	assert.Equal(t, []string{"application/merge-patch+json"}, api.patchTypes)

	require.NoError(t, s.RemoveKeys(context.Background(), "opengate-secrets", "ca.crt", "ca.key"),
		"removing keys already gone succeeds, so an interrupted move can run again")
}

func TestRemoveKeysOnAMissingSecretIsRefused(t *testing.T) {
	srv := httptest.NewServer(&apiServer{secrets: map[string]secretObject{}})
	t.Cleanup(srv.Close)

	err := NewSecrets(srv.URL, "opengate", "t", srv.Client()).RemoveKeys(context.Background(), "opengate-secrets", "ca.key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestSecretsRoundTripCreatingThenReplacing(t *testing.T) {
	api := &apiServer{secrets: map[string]secretObject{}}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	s := NewSecrets(srv.URL, "opengate", "pod-token", srv.Client())
	ctx := context.Background()

	_, found, err := s.Read(ctx, "opengate-authority")
	require.NoError(t, err)
	assert.False(t, found)

	first := steady(t)
	require.NoError(t, s.Write(ctx, "opengate-authority", first))
	got, found, err := s.Read(ctx, "opengate-authority")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, first, got)

	staged, err := NewKit(got, now)
	require.NoError(t, err)
	require.NoError(t, s.Write(ctx, "opengate-authority", staged))
	got, _, err = s.Read(ctx, "opengate-authority")
	require.NoError(t, err)
	assert.NotEmpty(t, got[NextKeyFile])
	for _, token := range api.tokens {
		assert.Equal(t, "Bearer pod-token", token)
	}
}

func TestARefusedWriteNamesTheStatusAndNotTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden: EC PRIVATE KEY"))
	}))
	t.Cleanup(srv.Close)
	err := NewSecrets(srv.URL, "opengate", "t", srv.Client()).Write(context.Background(), "opengate-authority", steady(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.NotContains(t, err.Error(), "PRIVATE KEY")
}

// serviceAccount writes the credentials Kubernetes mounts into a pod, for an API server at srv.
func serviceAccount(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	dir := t.TempDir()
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token"), []byte("pod-token\n"), 0o400))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "namespace"), []byte("opengate\n"), 0o400))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ca.crt"), caPEM, 0o400))
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	t.Setenv("KUBERNETES_SERVICE_HOST", u.Hostname())
	t.Setenv("KUBERNETES_SERVICE_PORT", u.Port())
	return dir
}

func TestInClusterTalksToTheAPIServerItIsMountedFor(t *testing.T) {
	api := &apiServer{secrets: map[string]secretObject{}}
	srv := httptest.NewTLSServer(api)
	t.Cleanup(srv.Close)
	s, err := inCluster(serviceAccount(t, srv))
	require.NoError(t, err)

	require.NoError(t, s.Write(context.Background(), "opengate-authority", steady(t)))
	assert.Contains(t, api.secrets, "opengate-authority")
	assert.Equal(t, "Bearer pod-token", api.tokens[0])
}

func TestInClusterOutsideAClusterSaysWhy(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		name  string
		spoil func(dir string)
		want  string
	}{
		{"no token", func(dir string) { _ = os.Remove(filepath.Join(dir, "token")) }, "token"},
		{"no namespace", func(dir string) { _ = os.Remove(filepath.Join(dir, "namespace")) }, "namespace"},
		{"no authority", func(dir string) { _ = os.Remove(filepath.Join(dir, "ca.crt")) }, "cluster authority"},
		{"bad authority", func(dir string) {
			_ = os.Remove(filepath.Join(dir, "ca.crt"))
			_ = os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("junk"), 0o400)
		}, "does not parse"},
		{"no address", func(string) { t.Setenv("KUBERNETES_SERVICE_HOST", "") }, "KUBERNETES_SERVICE_HOST"},
		{"no mount", func(dir string) { _ = os.RemoveAll(dir) }, "service-account mount"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := serviceAccount(t, srv)
			tc.spoil(dir)
			_, err := inCluster(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestAnAPIServerThatCannotBeReachedIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	s := NewSecrets(srv.URL, "opengate", "t", srv.Client())
	_, _, err := s.Read(context.Background(), "opengate-authority")
	assert.Error(t, err)
	assert.Error(t, s.RemoveKeys(context.Background(), "opengate-secrets", "ca.key"))
}

func TestAnAnswerThatIsNotASecretIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>"))
	}))
	t.Cleanup(srv.Close)
	_, _, err := NewSecrets(srv.URL, "opengate", "t", srv.Client()).Read(context.Background(), "opengate-authority")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestAReadTheAPIServerRefusesNamesTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	_, _, err := NewSecrets(srv.URL, "opengate", "t", srv.Client()).Read(context.Background(), "opengate-authority")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
