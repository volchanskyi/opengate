package updater

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const defaultGitHubAPI = "https://api.github.com"

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// SyncFromGitHub fetches the latest GitHub release for the given repo and
// publishes manifests for each agent binary asset found.
// apiBase overrides the GitHub API base URL (for testing); pass "" for default.
func SyncFromGitHub(ctx context.Context, repo, apiBase string, signing *SigningKeys, store *ManifestStore) ([]*Manifest, error) {
	if repo == "" {
		return nil, fmt.Errorf("github repo is required")
	}
	if apiBase == "" {
		apiBase = defaultGitHubAPI
	}

	url := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}

	version := strings.TrimPrefix(release.TagName, "v")

	// Index assets by name for fast lookup.
	assetByName := make(map[string]ghAsset, len(release.Assets))
	for _, a := range release.Assets {
		assetByName[a.Name] = a
	}

	// Known platforms to look for.
	type platform struct{ os, arch string }
	platforms := []platform{
		{"linux", "amd64"},
		{"linux", "arm64"},
	}

	var synced []*Manifest
	for _, p := range platforms {
		binaryName := fmt.Sprintf("mesh-agent-%s-%s", p.os, p.arch)
		sha256Name := binaryName + ".sha256"

		binary, hasBinary := assetByName[binaryName]
		sha256Asset, hasSHA := assetByName[sha256Name]
		if !hasBinary || !hasSHA {
			continue
		}

		sha256Hex, err := fetchSHA256(ctx, sha256Asset.BrowserDownloadURL)
		if err != nil {
			return nil, fmt.Errorf("fetch sha256 for %s: %w", binaryName, err)
		}

		sig, err := signing.SignHash(sha256Hex)
		if err != nil {
			return nil, fmt.Errorf("sign %s: %w", binaryName, err)
		}

		m := &Manifest{
			Version:   version,
			OS:        p.os,
			Arch:      p.arch,
			URL:       binary.BrowserDownloadURL,
			SHA256:    sha256Hex,
			Signature: sig,
			CreatedAt: time.Now().UTC(),
		}

		if err := store.Put(ctx, m); err != nil {
			return nil, fmt.Errorf("store manifest %s/%s: %w", p.os, p.arch, err)
		}
		synced = append(synced, m)
	}

	return synced, nil
}

// StartPeriodicSync runs SyncFromGitHub immediately and then on the given
// interval until ctx is cancelled. Pass interval=0 for the default (1 hour).
func StartPeriodicSync(ctx context.Context, repo string, interval time.Duration, signing *SigningKeys, store *ManifestStore, logger *slog.Logger) {
	if interval == 0 {
		interval = time.Hour
	}
	sync := func() {
		syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		synced, err := SyncFromGitHub(syncCtx, repo, "", signing, store)
		if err != nil {
			logger.Warn("github manifest sync failed", "repo", repo, "error", err)
		} else {
			logger.Info("synced manifests from github", "repo", repo, "count", len(synced))
		}
	}

	// Initial sync immediately.
	sync()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sync()
		}
	}
}

// fetchSHA256 downloads a .sha256 file and returns the hex digest.
func fetchSHA256(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", err
	}

	// Format: "<hex>  <filename>\n" or just "<hex>\n"
	line := strings.TrimSpace(string(body))
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", fmt.Errorf("empty sha256 file")
	}

	hexStr := fields[0]
	// Validate it's a proper 64-char hex string.
	if len(hexStr) != 64 {
		return "", fmt.Errorf("invalid sha256 length: got %d chars", len(hexStr))
	}
	if _, err := hex.DecodeString(hexStr); err != nil {
		return "", fmt.Errorf("invalid sha256 hex: %w", err)
	}

	return hexStr, nil
}
