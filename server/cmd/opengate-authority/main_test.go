package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/authority"
	"github.com/volchanskyi/opengate/server/internal/cert"
)

var clock = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

// kubeAPI stands in for the Kubernetes API's secrets in the namespace the job runs in.
type kubeAPI struct {
	mu      sync.Mutex
	secrets map[string]map[string][]byte
	// refuse answers 403 to every method it names, as a role without that verb would.
	refuse map[string]bool
}

type wireSecret struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Data map[string][]byte `json:"data"`
}

func (k *kubeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.mu.Lock()
	defer k.mu.Unlock()
	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/opengate/secrets"), "/")
	body, _ := io.ReadAll(r.Body)
	if k.refuse[r.Method] {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, ok := k.secrets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var obj wireSecret
		obj.Metadata.Name, obj.Data = name, data
		_ = json.NewEncoder(w).Encode(obj)
	case http.MethodPost, http.MethodPut:
		var obj wireSecret
		_ = json.Unmarshal(body, &obj)
		k.secrets[obj.Metadata.Name] = obj.Data
		w.WriteHeader(map[string]int{http.MethodPost: http.StatusCreated, http.MethodPut: http.StatusOK}[r.Method])
	case http.MethodPatch:
		data, ok := k.secrets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var patch struct {
			Data map[string]*[]byte `json:"data"`
		}
		_ = json.Unmarshal(body, &patch)
		for key := range patch.Data {
			delete(data, key)
		}
		w.WriteHeader(http.StatusOK)
	}
}

// job runs the command against the stand-in API and returns what it printed.
type job struct {
	t   *testing.T
	api *kubeAPI
	srv *httptest.Server
}

func newJob(t *testing.T) *job {
	t.Helper()
	api := &kubeAPI{secrets: map[string]map[string][]byte{}}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return &job{t: t, api: api, srv: srv}
}

func (j *job) run(args ...string) (string, error) {
	var out bytes.Buffer
	e := env{
		out: &out,
		now: func() time.Time { return clock },
		connect: func() (*authority.Secrets, error) {
			return authority.NewSecrets(j.srv.URL, "opengate", "pod-token", j.srv.Client()), nil
		},
	}
	err := e.run(context.Background(), args)
	return out.String(), err
}

func (j *job) mustRun(args ...string) string {
	j.t.Helper()
	out, err := j.run(args...)
	require.NoError(j.t, err, out)
	return out
}

func (j *job) secret(name string) map[string][]byte {
	j.api.mu.Lock()
	defer j.api.mu.Unlock()
	return j.api.secrets[name]
}

// legacyFolder writes the master key into a folder as the shared secret's volume presents it.
func legacyFolder(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{cert.IssuerCertFile, cert.IssuerKeyFile} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), files[name], 0o400))
	}
	return dir
}

func newKey(t *testing.T) map[string][]byte {
	t.Helper()
	files, err := authority.Init(authority.Files{}, clock)
	require.NoError(t, err)
	return files
}

func TestInitCreatesTheMasterKeyAndPrintsNoKeyMaterial(t *testing.T) {
	j := newJob(t)
	out := j.mustRun("init")

	got := j.secret("opengate-authority")
	assert.NotEmpty(t, got[cert.IssuerCertFile])
	assert.NotEmpty(t, got[cert.IssuerKeyFile])
	assert.Contains(t, out, "issuer")
	assert.NotContains(t, out, "PRIVATE KEY")
}

func TestInitRefusesAnExistingMasterKey(t *testing.T) {
	j := newJob(t)
	j.mustRun("init")
	before := j.secret("opengate-authority")[cert.IssuerKeyFile]

	_, err := j.run("init")
	require.ErrorIs(t, err, authority.ErrKeyExists)
	assert.Equal(t, before, j.secret("opengate-authority")[cert.IssuerKeyFile])
}

func TestKeyChangeWalksFromKitToSettle(t *testing.T) {
	j := newJob(t)
	j.mustRun("init")
	first := j.secret("opengate-authority")[cert.IssuerCertFile]

	out := j.mustRun("new-kit")
	assert.Contains(t, out, "next (staged)")
	assert.Equal(t, first, j.secret("opengate-authority")[cert.IssuerCertFile], "a kit changes no role")

	out = j.mustRun("layout", "-phase", "changeover")
	assert.Contains(t, out, "accepted")
	assert.Contains(t, out, "restart the server")
	assert.Equal(t, first, j.secret("opengate-authority")[cert.AcceptedFile])

	j.mustRun("layout", "-phase", "retire")
	assert.Equal(t, first, j.secret("opengate-authority")[cert.RenewOnlyFile])

	j.mustRun("layout", "-phase", "settle", "-drop-old")
	settled := j.secret("opengate-authority")
	assert.NotEqual(t, first, settled[cert.IssuerCertFile])
	assert.Empty(t, settled[cert.RenewOnlyFile])
	assert.Empty(t, settled[cert.ChainFile])
}

func TestLayoutRefusesAnUnknownPhase(t *testing.T) {
	j := newJob(t)
	j.mustRun("init")
	_, err := j.run("layout", "-phase", "sideways")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sideways")
}

func TestLayoutMovesAMTToTheNewKeyOnlyWhenAsked(t *testing.T) {
	j := newJob(t)
	j.mustRun("init")
	j.mustRun("new-kit")
	j.mustRun("layout", "-phase", "changeover", "-mps", "new")
	assert.Empty(t, j.secret("opengate-authority")[cert.MPSIssuerKeyFile])

	_, err := j.run("layout", "-phase", "retire", "-mps", "sideways")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-mps")
}

func TestInspectOfAMissingSecretNamesInit(t *testing.T) {
	_, err := newJob(t).run("inspect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authority-init")
}

func TestInspectPrintsTheRolesOfTheNamedSecret(t *testing.T) {
	j := newJob(t)
	j.mustRun("init", "-secret", "other-authority")
	out := j.mustRun("inspect", "-secret", "other-authority")
	assert.Contains(t, out, "issuer")
	assert.Nil(t, j.secret("opengate-authority"))
}

func TestStageCopiesTheMountedRoleFilesOwnerOnly(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(from, cert.IssuerKeyFile), []byte("key"), 0o440))

	_, err := newJob(t).run("stage", "-from", from, "-to", to)
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(to, cert.IssuerKeyFile))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o400), info.Mode().Perm())
}

func TestStageNeedsBothFolders(t *testing.T) {
	_, err := newJob(t).run("stage", "-from", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-to")
}

func TestAnUnknownCommandPrintsUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"rotate"}} {
		out, err := newJob(t).run(args...)
		require.Error(t, err)
		assert.Contains(t, out, "Usage: opengate-authority")
	}
}

func TestAJobOutsideAClusterSaysSo(t *testing.T) {
	e := env{
		out:     io.Discard,
		now:     time.Now,
		connect: func() (*authority.Secrets, error) { return nil, errors.New("not inside a cluster") },
	}
	err := e.run(context.Background(), []string{"inspect"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not inside a cluster")
}

func TestARefusedReadStopsEveryCommand(t *testing.T) {
	for _, args := range [][]string{{"init"}, {"adopt", "-legacy-dir", "."}, {"inspect"}} {
		j := newJob(t)
		j.api.refuse = map[string]bool{http.MethodGet: true}
		_, err := j.run(args...)
		require.Error(t, err, args)
		assert.Contains(t, err.Error(), "403", args)
	}
}

func TestAnUnknownFlagIsRefused(t *testing.T) {
	for _, args := range [][]string{{"inspect", "-sideways"}, {"stage", "-sideways"}} {
		_, err := newJob(t).run(args...)
		assert.Error(t, err, args)
	}
}
