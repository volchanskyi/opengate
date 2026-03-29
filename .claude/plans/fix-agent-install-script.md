# Fix: Agent Binary Distribution — Install Script + Auto-Sync Manifests

## Context
The setup page at `/setup` shows "No agent binaries published for this platform yet." GitHub release v0.1.1 exists with linux-amd64 and linux-arm64 binaries, but the manifests were never published to the server (requires manual admin action at `/admin/updates`). This plan fixes both the install flow and the setup page by:
1. Making `install.sh` download directly from GitHub Releases (no manifest dependency)
2. Auto-syncing manifests from GitHub on server startup (fixes the setup page UI)
3. Making the install script downloadable from the setup page UI

## Constraints
- **No hardcoded values** — all config via env vars (e.g., `OPENGATE_GITHUB_REPO`)
- **TDD** — write failing tests first, then implement
- **Skip `/precommit` + `/refactor`** — user will trigger manually

---

## Part 1: Server Auto-Sync Manifests from GitHub (TDD)

### 1a. New file: `server/internal/updater/github_test.go` (write first)
Tests using `httptest.NewServer` to mock GitHub API:
- `TestSyncFromGitHub_Success` — mock latest release with 2 binary + 2 sha256 assets → 2 manifests stored, signed
- `TestSyncFromGitHub_NoAssets` — release exists but no matching assets → returns empty, no error
- `TestSyncFromGitHub_APIError` — 500 response → returns error
- `TestSyncFromGitHub_EmptyRepo` — empty repo string → returns error
- `TestSyncFromGitHub_MalformedSHA256` — .sha256 file with bad content → returns error

### 1b. New file: `server/internal/updater/github.go`
```go
// SyncFromGitHub fetches the latest GitHub release and publishes manifests.
// apiBase is injectable for testing (defaults to "https://api.github.com" if empty).
func SyncFromGitHub(ctx context.Context, repo string, apiBase string, signing *SigningKeys, store *ManifestStore) ([]*Manifest, error)
```
- `GET {apiBase}/repos/{repo}/releases/latest`
- Parse JSON: tag_name, assets array
- Match assets by name: `mesh-agent-{os}-{arch}` and `mesh-agent-{os}-{arch}.sha256`
- Download `.sha256` content (small text), parse hex digest (first field before space)
- Version = tag_name with `v` prefix stripped
- URL = asset `browser_download_url`
- Sign with `signing.SignHash(sha256)`
- Store via `store.Put()`
- No new dependencies (`net/http`, `encoding/json`, `strings`, `fmt`)

### 1c. Modified: `server/internal/api/api.go` (+2 lines)
- Add `GitHubRepo string` to `ServerConfig`
- Add `githubRepo string` to `Server` struct, set in `NewServer()`

### 1d. Modified: `server/cmd/meshserver/main.go`
- Read `OPENGATE_GITHUB_REPO` env var (no flag, no default)
- Pass to `ServerConfig.GitHubRepo`
- After server init, if `GitHubRepo != ""`:
  ```go
  go func() {
      if synced, err := updater.SyncFromGitHub(ctx, repo, "", signingKeys, manifestStore); err != nil {
          logger.Warn("github manifest sync failed", "error", err)
      } else {
          logger.Info("synced manifests from github", "count", len(synced))
      }
  }()
  ```
- Non-blocking, log-only, no startup failure

---

## Part 2: Fix `install.sh` — Download from GitHub Releases (TDD)

### 2a. Modified: `server/internal/api/install.sh`

**Current problem**: Lines 77-91 call server manifest API. Fails if no manifests published.

**New download logic** (replaces lines 75-101):
1. Check `OPENGATE_GITHUB_REPO` env var
2. If set: call GitHub Releases API to get binary URL + SHA256
   - `GET https://api.github.com/repos/${OPENGATE_GITHUB_REPO}/releases/latest`
   - Parse with python3 for asset matching `mesh-agent-${OS}-${ARCH}`
   - Download `.sha256` companion asset
3. If not set OR GitHub unreachable: fall back to server manifest API (existing logic)
4. Rest of script unchanged (download, verify SHA256, install, enroll, systemd)

### 2b. Modified: `server/internal/api/handlers_install_test.go`
- Test that `GET /api/v1/server/install.sh` returns the script with correct content-type
- Test script contains GitHub Releases logic

---

## Part 3: Setup Page — Downloadable Install Script

### 3a. Modified: `web/src/features/agent-setup/AgentSetupPage.tsx`
- Add a "Download Install Script" button/link pointing to `/api/v1/server/install.sh`
- Button sits in the Manual Install section
- Uses `<a href="/api/v1/server/install.sh" download="install.sh">` for direct download

---

## File Summary

| File | Action | Lines (~) |
|------|--------|-----------|
| `server/internal/updater/github_test.go` | New (tests first) | ~120 |
| `server/internal/updater/github.go` | New | ~80 |
| `server/internal/api/api.go` | Modify | +2 |
| `server/cmd/meshserver/main.go` | Modify | +10 |
| `server/internal/api/install.sh` | Modify | ~30 changed |
| `web/src/features/agent-setup/AgentSetupPage.tsx` | Modify | +5 |

## What does NOT change
- No new API endpoints (install.sh endpoint already exists)
- No OpenAPI spec changes
- No new Go dependencies
- Manifest publish + push endpoints unchanged

## Verification
1. `go test ./server/internal/updater/...` — new tests pass
2. `make test` — all tests pass
3. `make lint` — clean
4. Set `OPENGATE_GITHUB_REPO=volchanskyi/opengate`, restart server → manifests auto-synced
5. Visit `/setup` → version + download link shown, install script downloadable
6. Run `install.sh <TOKEN>` on a test machine → agent installs from GitHub Releases
