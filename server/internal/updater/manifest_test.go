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

func testManifest(version, osName, arch, tag string) *Manifest {
	return &Manifest{
		Version: version, OS: osName, Arch: arch,
		URL: "u" + tag, SHA256: "h" + tag, Signature: "s" + tag,
		CreatedAt: time.Now().UTC(),
	}
}

func writeCorruptManifest(t *testing.T, content string) *ManifestStore {
	t.Helper()
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "manifests")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "linux-amd64.json"), []byte(content), 0644))
	return NewManifestStore(dir)
}

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

	got, err := store.Get(context.Background(), "linux", "amd64")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestManifestStore_List(t *testing.T) {
	store := NewManifestStore(t.TempDir())
	ctx := context.Background()

	for _, m := range []*Manifest{
		testManifest("1.0.0", "linux", "amd64", "1"),
		testManifest("1.0.0", "linux", "arm64", "2"),
	} {
		require.NoError(t, store.Put(ctx, m))
	}

	list, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestManifestStore_PutOverwrites(t *testing.T) {
	store := NewManifestStore(t.TempDir())
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, testManifest("1.0.0", "linux", "amd64", "1")))
	require.NoError(t, store.Put(ctx, testManifest("2.0.0", "linux", "amd64", "2")))

	got, err := store.Get(ctx, "linux", "amd64")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "2.0.0", got.Version)
	assert.Equal(t, "u2", got.URL)
}

func TestManifestStore_GetCorruptedFile(t *testing.T) {
	store := writeCorruptManifest(t, "not json")

	_, err := store.Get(context.Background(), "linux", "amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse manifest")
}

func TestManifestStore_ListCorruptedFile(t *testing.T) {
	store := writeCorruptManifest(t, "{bad")

	_, err := store.List(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestManifestStore_SafePath_RejectsTraversal(t *testing.T) {
	store := NewManifestStore(t.TempDir())
	ctx := context.Background()

	// Every case cleans to a path outside the store directory.
	tests := []struct {
		name string
		os   string
		arch string
	}{
		{"dot-dot prefix in OS", "../etc", "amd64"},
		{"deep traversal in OS", "a/../../secret", "amd64"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := store.Put(ctx, testManifest("1.0.0", tc.os, tc.arch, ""))
			assert.Error(t, err, "Put should reject traversal path %s/%s", tc.os, tc.arch)

			got, err := store.Get(ctx, tc.os, tc.arch)
			assert.Error(t, err, "Get should reject traversal path %s/%s", tc.os, tc.arch)
			assert.Nil(t, got)
		})
	}
}

func TestManifestStore_SafePath_AllowsValidNames(t *testing.T) {
	store := NewManifestStore(t.TempDir())
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, testManifest("1.0.0", "linux", "amd64", "")))

	got, err := store.Get(ctx, "linux", "amd64")
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", got.Version)
}

func TestManifestStore_ListEmpty(t *testing.T) {
	store := NewManifestStore(t.TempDir())

	list, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Nil(t, list)
}
