package authority

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

func TestAdoptCopiesTheLegacyKeyOnceAndOnlyOnce(t *testing.T) {
	legacy := steady(t)
	files, adopted, err := Adopt(Files{}, legacy)
	require.NoError(t, err)
	assert.True(t, adopted)
	assert.Equal(t, legacy[cert.IssuerKeyFile], files[cert.IssuerKeyFile])

	_, adopted, err = Adopt(files, steady(t))
	require.NoError(t, err)
	assert.False(t, adopted, "an existing master key is never replaced")
}

func TestStageCopiesOwnerOnly(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	writeFiles(t, from, steady(t), 0o440)
	require.NoError(t, Stage(from, to))
	for _, name := range []string{cert.IssuerCertFile, cert.IssuerKeyFile} {
		info, err := os.Stat(filepath.Join(to, name))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o400), info.Mode().Perm(), name)
	}
	_, err := cert.NewManager(to, cert.WithMustExist())
	assert.NoError(t, err, "the server starts on what was staged")
}

func TestStageReadsASecretVolumesLinkedLayout(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	version := filepath.Join(from, "..2026_10_16_12_00_00.000000001")
	require.NoError(t, os.Mkdir(version, 0o755))
	writeFiles(t, version, steady(t), 0o440)
	require.NoError(t, os.Symlink(filepath.Base(version), filepath.Join(from, "..data")))
	for _, name := range []string{cert.IssuerCertFile, cert.IssuerKeyFile} {
		require.NoError(t, os.Symlink(filepath.Join("..data", name), filepath.Join(from, name)))
	}

	require.NoError(t, Stage(from, to))
	entries, err := os.ReadDir(to)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{cert.IssuerCertFile, cert.IssuerKeyFile}, names, "only the current files, no version folders")
	_, err = cert.NewManager(to, cert.WithMustExist())
	assert.NoError(t, err)
}

func TestStageRefusesAFolderItCannotRead(t *testing.T) {
	err := Stage(filepath.Join(t.TempDir(), "absent"), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absent")
}

func TestStageRefusesAFolderItCannotWrite(t *testing.T) {
	from := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(from, cert.IssuerKeyFile), []byte("k"), 0o400))
	to := filepath.Join(t.TempDir(), "absent")
	err := Stage(from, to)
	require.Error(t, err)
	assert.Contains(t, err.Error(), to)
	assert.NoFileExists(t, filepath.Join(to, cert.IssuerKeyFile))
}
