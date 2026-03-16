package updater

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifestStore_PutAndGet(t *testing.T) {
	store := NewManifestStore(t.TempDir())
	ctx := context.Background()

	m := &Manifest{
		Version:   "1.0.0",
		OS:        "linux",
		Arch:      "amd64",
		URL:       "https://example.com/agent-1.0.0-linux-amd64",
		SHA256:    "abcdef1234567890",
		Signature: "sig1234",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}

	require.NoError(t, store.Put(ctx, m))

	got, err := store.Get(ctx, "linux", "amd64")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, m.Version, got.Version)
	assert.Equal(t, m.OS, got.OS)
	assert.Equal(t, m.Arch, got.Arch)
	assert.Equal(t, m.URL, got.URL)
	assert.Equal(t, m.SHA256, got.SHA256)
	assert.Equal(t, m.Signature, got.Signature)
}

func TestManifestStore_GetMissing(t *testing.T) {
	store := NewManifestStore(t.TempDir())
	ctx := context.Background()

	got, err := store.Get(ctx, "linux", "amd64")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestManifestStore_List(t *testing.T) {
	store := NewManifestStore(t.TempDir())
	ctx := context.Background()

	manifests := []*Manifest{
		{Version: "1.0.0", OS: "linux", Arch: "amd64", URL: "u1", SHA256: "h1", Signature: "s1", CreatedAt: time.Now().UTC()},
		{Version: "1.0.0", OS: "linux", Arch: "arm64", URL: "u2", SHA256: "h2", Signature: "s2", CreatedAt: time.Now().UTC()},
	}
	for _, m := range manifests {
		require.NoError(t, store.Put(ctx, m))
	}

	list, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestManifestStore_PutOverwrites(t *testing.T) {
	store := NewManifestStore(t.TempDir())
	ctx := context.Background()

	m1 := &Manifest{Version: "1.0.0", OS: "linux", Arch: "amd64", URL: "u1", SHA256: "h1", Signature: "s1", CreatedAt: time.Now().UTC()}
	require.NoError(t, store.Put(ctx, m1))

	m2 := &Manifest{Version: "2.0.0", OS: "linux", Arch: "amd64", URL: "u2", SHA256: "h2", Signature: "s2", CreatedAt: time.Now().UTC()}
	require.NoError(t, store.Put(ctx, m2))

	got, err := store.Get(ctx, "linux", "amd64")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "2.0.0", got.Version)
	assert.Equal(t, "u2", got.URL)
}

func TestManifestStore_GetCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	store := NewManifestStore(dir)

	// Write a corrupt manifest file directly
	manifestDir := filepath.Join(dir, "manifests")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "linux-amd64.json"), []byte("not json"), 0644))

	_, err := store.Get(context.Background(), "linux", "amd64")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse manifest")
}

func TestManifestStore_ListCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	store := NewManifestStore(dir)

	manifestDir := filepath.Join(dir, "manifests")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "linux-amd64.json"), []byte("{bad"), 0644))

	_, err := store.List(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestManifestStore_ListEmpty(t *testing.T) {
	store := NewManifestStore(t.TempDir())
	ctx := context.Background()

	list, err := store.List(ctx)
	require.NoError(t, err)
	assert.Nil(t, list)
}
