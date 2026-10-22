package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/authority"
	"github.com/volchanskyi/opengate/server/internal/cert"
)

// share keeps a master key in the shared secret, where adopt finds it.
func (j *job) share(key map[string][]byte) {
	j.api.secrets[sharedSecret] = map[string][]byte{
		cert.IssuerCertFile: key[cert.IssuerCertFile], cert.IssuerKeyFile: key[cert.IssuerKeyFile],
	}
}

func TestAdoptMovesTheSharedKeyIntoItsOwnSecret(t *testing.T) {
	j := newJob(t)
	key := newKey(t)
	j.share(key)
	j.api.secrets[sharedSecret]["JWT_SECRET"] = []byte("j")

	out := j.mustRun("adopt", "-legacy-dir", legacyFolder(t, key))

	assert.Equal(t, key[cert.IssuerKeyFile], j.secret("opengate-authority")[cert.IssuerKeyFile])
	assert.Equal(t, map[string][]byte{"JWT_SECRET": []byte("j")}, j.secret(sharedSecret))
	assert.Contains(t, out, "issuer")
	assert.NotContains(t, out, "PRIVATE KEY")
}

func TestAdoptAgainChangesNothing(t *testing.T) {
	j := newJob(t)
	key := newKey(t)
	j.share(key)
	legacy := legacyFolder(t, key)
	j.mustRun("adopt", "-legacy-dir", legacy)

	j.mustRun("adopt", "-legacy-dir", legacy)
	j.mustRun("adopt", "-legacy-dir", t.TempDir())

	assert.Equal(t, key[cert.IssuerKeyFile], j.secret("opengate-authority")[cert.IssuerKeyFile])
	assert.Empty(t, j.secret(sharedSecret))
}

func TestAdoptRefusesASharedKeyThatDiffersFromItsOwn(t *testing.T) {
	j := newJob(t)
	j.mustRun("init")
	other := newKey(t)
	j.share(other)

	_, err := j.run("adopt", "-legacy-dir", legacyFolder(t, other))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differs")
	assert.Equal(t, other[cert.IssuerKeyFile], j.secret(sharedSecret)[cert.IssuerKeyFile],
		"a key nobody has adopted stays where it is")
}

func TestAdoptWithNoKeyAnywhereNamesInit(t *testing.T) {
	j := newJob(t)
	_, err := j.run("adopt", "-legacy-dir", t.TempDir())
	require.ErrorIs(t, err, authority.ErrNoIssuer)
	assert.Contains(t, err.Error(), "authority-init")
	assert.Nil(t, j.secret("opengate-authority"))
}

func TestAdoptNeedsTheLegacyFolder(t *testing.T) {
	_, err := newJob(t).run("adopt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-legacy-dir")
}

func TestARefusedWriteLeavesTheSharedKeyInPlace(t *testing.T) {
	j := newJob(t)
	key := newKey(t)
	j.share(key)
	j.api.refuse = map[string]bool{http.MethodPost: true}

	_, err := j.run("adopt", "-legacy-dir", legacyFolder(t, key))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Equal(t, key[cert.IssuerKeyFile], j.secret(sharedSecret)[cert.IssuerKeyFile])
}

func TestARefusedRemovalFailsTheAdoption(t *testing.T) {
	j := newJob(t)
	key := newKey(t)
	j.api.secrets[sharedSecret] = map[string][]byte{cert.IssuerKeyFile: key[cert.IssuerKeyFile]}
	j.api.refuse = map[string]bool{http.MethodPatch: true}

	_, err := j.run("adopt", "-legacy-dir", legacyFolder(t, key))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
