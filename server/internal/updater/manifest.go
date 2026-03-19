package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Manifest describes a released agent binary version for a specific OS/arch.
type Manifest struct {
	Version   string    `json:"version"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	URL       string    `json:"url"`
	SHA256    string    `json:"sha256"`
	Signature string    `json:"signature"`
	CreatedAt time.Time `json:"created_at"`
}

// ManifestStore manages on-disk agent update manifests.
type ManifestStore struct {
	dir string
}

// NewManifestStore creates a manifest store rooted at {dataDir}/manifests/.
func NewManifestStore(dataDir string) *ManifestStore {
	return &ManifestStore{dir: filepath.Join(dataDir, "manifests")}
}

// Put writes a manifest for the given OS/arch combination.
func (s *ManifestStore) Put(_ context.Context, m *Manifest) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	path, err := s.safePath(m.OS, m.Arch)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

// Get returns the current manifest for the given OS/arch, or nil if none exists.
func (s *ManifestStore) Get(_ context.Context, osName, arch string) (*Manifest, error) {
	path, err := s.safePath(osName, arch)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// List returns all current manifests.
func (s *ManifestStore) List(_ context.Context) ([]*Manifest, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list manifests: %w", err)
	}

	var manifests []*Manifest
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		manifests = append(manifests, &m)
	}
	return manifests, nil
}

// safePath constructs a manifest file path from OS/arch values and validates
// the result stays within the store directory to prevent path traversal.
func (s *ManifestStore) safePath(osName, arch string) (string, error) {
	name := manifestFilename(osName, arch)
	cleanPath := filepath.Join(s.dir, filepath.Clean(name))
	if !strings.HasPrefix(cleanPath, filepath.Clean(s.dir)+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid manifest path: %s/%s", osName, arch)
	}
	return cleanPath, nil
}

func manifestFilename(osName, arch string) string {
	return osName + "-" + arch + ".json"
}
