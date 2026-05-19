# Path-gate `release-agent.yml` + add agent-side content-hash precheck

## Context

Every merge to `main` whose merged commits include any `feat:` or `fix:` prefix triggers a chain in [.github/workflows/ci.yml](../../.github/workflows/ci.yml):

1. The `auto-tag` job ([ci.yml:1411-1542](../../.github/workflows/ci.yml#L1411-L1542)) bumps semver based on the conventional-commit prefix.
2. It pushes the new `vX.Y.Z` tag.
3. It dispatches [`release-agent.yml`](../../.github/workflows/release-agent.yml), which cross-builds the Rust agent for `x86_64-unknown-linux-musl` + `aarch64-unknown-linux-musl` and creates a GitHub Release with binaries.

The decision **never looks at the diff path** — it's purely conventional-commit prefix matching. Of the last 50 commits on `main`, only **7 (14%) actually touched `agent/`** ([git log audit][1]). The other 86% (server, web, deploy, docs, CI) currently produce spurious agent releases.

### Cost today (corrected after audit)

- **~15-20 min of CI wall-clock per spurious release** (arm64 `cross` compile dominates).
- **Release-page noise** — version bumps with no agent assets that matter.
- **NOT bandwidth from agent downloads** — manifest publishing via `POST /api/v1/updates/manifests` ([server/internal/api/handlers_updates.go:32-64](../../server/internal/api/handlers_updates.go#L32-L64)) is currently **manual**. `release-agent.yml` does not auto-publish; fielded agents do not auto-download spurious releases. *Yet.*

### Cost when project grows (the load-bearing reason to fix now)

- A near-term enhancement to auto-publish the manifest from `release-agent.yml` is obvious — it closes the operational gap of "binary exists in GitHub Releases but agents don't see it." The moment that lands, every spurious release becomes a fleet-wide push of a near-identical binary (10–50 MB × N agents), because:
  - The agent compares **version strings only** in `should_skip_version` ([agent/crates/mesh-agent/src/main.rs:835-843](../../agent/crates/mesh-agent/src/main.rs#L835-L843)) — no hash precheck.
  - Binaries are **not deterministic** (no `[profile.release]` pin, no `SOURCE_DATE_EPOCH`, dynamic `cross` install) — so identical source produces different bytes, defeating any "hash-prove-it's-the-same" claim.
- Fleet-multiplier: at N=100 agents this is 1–5 GB of avoidable egress per spurious release; at N=1000, 10–50 GB.

### Outcome

Two complementary defenses that compose safely with the future auto-publish enhancement:

1. **Workflow gate** — `release-agent.yml` diffs `agent/**` against the previous `v*` tag at workflow entry and skips the matrix build (and therefore the release) when the subtree is unchanged.
2. **Agent-side content-hash precheck** — `apply_update` compares the manifest's `sha256` against the SHA-256 of the currently-running binary BEFORE downloading. If they match, skip the download + swap entirely (returns `Ok(false)`). Belt-and-suspenders for the day when the workflow gate is bypassed (manual dispatch with a wrong tag, future auto-publish that touches multiple workflows, etc.).

[1]: git log --oneline -50 main | grep -E "^[a-f0-9]+ (feat|fix)" + per-commit `git show --stat`

---

## Implementation

### Part 1 — Workflow gate in `release-agent.yml`

**Idiom:** inline `git diff --name-only` against the previous tag. Matches the project convention from [`iac-gate` in ci.yml:1075-1136](../../.github/workflows/ci.yml#L1075-L1136). Do not introduce `dorny/paths-filter` (not used anywhere in the repo today).

**Insertion point:** new `check-agent-changed` job at the top of `release-agent.yml`, before `build`. Exposes `agent_changed` as a job output. `build` gets `needs: check-agent-changed` + `if: needs.check-agent-changed.outputs.agent_changed == 'true'`. `release` already `needs: build`, so a skipped build cleanly propagates to a skipped release (no GitHub Release entry is created — matches the chosen UX: "no release at all when skipped" so `/releases/latest` keeps pointing at the most recent binaries-bearing release).

**Baseline ref:** `git describe --tags --abbrev=0 --match 'v*' "${TAG}^"`. `^` skips the tagged commit so `describe` returns the prior `v*`, never the current one. First-ever release path (no prior tag) defaults to `agent_changed=true`.

**Idempotency:** the check is a pure function of git state (current tag + repo tree), so manual `workflow_dispatch` re-runs produce the same decision as the auto-dispatch from `auto-tag`. Use `inputs.tag || github.ref_name` (already the pattern at [release-agent.yml:33,56](../../.github/workflows/release-agent.yml#L33)).

**Forward-compat with auto-publish-manifest:** when a future PR adds a `publish-manifest` job to this same workflow, it joins as `needs: [check-agent-changed, release]` with `if: needs.check-agent-changed.outputs.agent_changed == 'true'` — the gate runs once and is consumed by all downstream jobs.

**Yaml diff** (insert at line 16, before the existing `build:` block at line 17, and amend the `build` job header):

```yaml
jobs:
  check-agent-changed:
    name: Check agent/ changed since previous tag
    runs-on: ubuntu-latest
    outputs:
      agent_changed: ${{ steps.diff.outputs.agent_changed }}
      prev_tag: ${{ steps.diff.outputs.prev_tag }}
    steps:
      - uses: actions/checkout@v6
        with:
          ref: ${{ inputs.tag || github.ref }}
          fetch-depth: 0
          fetch-tags: true
      - name: Diff agent/ against previous v* tag
        id: diff
        run: |
          set -euo pipefail
          TAG="${{ inputs.tag || github.ref_name }}"
          PREV=$(git describe --tags --abbrev=0 --match 'v*' "${TAG}^" 2>/dev/null || echo "")
          echo "prev_tag=${PREV}" >> "$GITHUB_OUTPUT"
          if [ -z "$PREV" ]; then
            echo "agent_changed=true" >> "$GITHUB_OUTPUT"
            echo "No previous v* tag — first release, building."
            exit 0
          fi
          if git diff --name-only "${PREV}" "${TAG}" -- 'agent/**' | grep -q .; then
            echo "agent_changed=true" >> "$GITHUB_OUTPUT"
            echo "agent/ changed between ${PREV} and ${TAG} — building."
          else
            echo "agent_changed=false" >> "$GITHUB_OUTPUT"
            echo "No agent/ changes between ${PREV} and ${TAG} — skipping."
          fi

  build:
    name: Build ${{ matrix.target }}
    needs: check-agent-changed
    if: needs.check-agent-changed.outputs.agent_changed == 'true'
    runs-on: ubuntu-latest
    # ... rest unchanged
```

**`ci.yml` `auto-tag` is unchanged** — keep dispatching unconditionally so the tag still flows to [`build-image.yml`](../../.github/workflows/build-image.yml) (server container image, separate plan).

### Part 2 — Agent-side content-hash precheck in `apply_update`

**Insertion point:** inside [`apply_update` in agent/crates/mesh-agent-core/src/update.rs:61-118](../../agent/crates/mesh-agent-core/src/update.rs#L61-L118), as a new step 0 BEFORE the download at line 73. Single source of truth — the caller in [main.rs:528-543](../../agent/crates/mesh-agent/src/main.rs#L528) already routes through `apply_update`, so all paths (push-from-server, future poll, manual trigger) get the precheck for free.

**Reuse existing helper:** `sha256_file` is already defined in the same file (used at line 76 to hash the downloaded binary). Call it on `config.current_binary_path` for the precheck — no new dependency, no new code.

**Logic:**

```rust
// 0. Precheck: if the current binary already matches the expected hash,
//    the update is a no-op (e.g. server re-published manifest with same
//    binary, or the workflow gate regressed and shipped a spurious tag).
//    Skip download + swap entirely to save bandwidth and rollback risk.
if !sha256_hex.is_empty() && config.current_binary_path.exists() {
    let current_hash = sha256_file(&config.current_binary_path).await?;
    if current_hash == sha256_hex {
        info!(
            version,
            sha256 = sha256_hex,
            "update precheck: current binary already matches manifest hash, skipping download"
        );
        return Ok(false);
    }
}
```

**Return value:** `Ok(false)` already means "skipped" per the existing docstring at update.rs:51. The caller's UpdateAck path ([main.rs:539-540](../../agent/crates/mesh-agent/src/main.rs#L539)) handles `false` the same as `should_skip_version=true` — sends a success ack with reason "already up to date".

**Edge cases:**
- `sha256_hex` empty → skip precheck (preserves existing behavior; old manifests without hash still work).
- `current_binary_path` doesn't exist → skip precheck (e.g. installed via a different path; fall through to download).
- Hash read fails (IO error) → propagate via `?`; the update aborts with a clear error. The watchdog won't fire because no rename happened.

### Part 3 — Tests (TDD, both sides)

**Workflow gate:** add a unit test for the new shell logic. Two options:
- (a) `scripts/tests/release-agent-gate.test.sh` — pure bash, plants a fake git history in a tmp dir, runs the gate snippet via `bash -c`, asserts outputs. Mirrors the existing `scripts/tests/tdd-check.test.sh` pattern.
- (b) Skip — rely on the workflow's first real execution as the test. NOT recommended; the project has a strong "no silent SKIP" rule and a regression here is silent (the workflow just doesn't run).

Pick (a). Three test cases:
1. No prior `v*` tag → output `true` (first-release safe).
2. Prior tag exists, `agent/` unchanged in the range → output `false`.
3. Prior tag exists, `agent/foo.rs` changed in the range → output `true`.

**Agent precheck:** extend [agent/crates/mesh-agent-core/src/update.rs](../../agent/crates/mesh-agent-core/src/update.rs) tests:
1. `current_binary_path` exists with hash `H`; manifest declares `H`; assert `apply_update` returns `Ok(false)` and `sha256_file` is called exactly once (no download initiated). Need to refactor `download_to_file` behind a small trait or use a never-served URL to detect.
2. `current_binary_path` exists with hash `H1`; manifest declares `H2`; assert download proceeds as today.
3. `sha256_hex` empty → precheck skipped, download proceeds.
4. `current_binary_path` missing → precheck skipped, download proceeds.

TDD ordering: write all four test cases first (they fail because the precheck doesn't exist), then add the precheck block in `apply_update`, watch them pass.

### Critical files to be modified / created

| File | Change |
|---|---|
| [`.github/workflows/release-agent.yml`](../../.github/workflows/release-agent.yml) | New `check-agent-changed` job; `build` gains `needs:` + `if:` (Part 1). |
| [`agent/crates/mesh-agent-core/src/update.rs`](../../agent/crates/mesh-agent-core/src/update.rs) | New step 0 in `apply_update` — sha256 precheck against current binary (Part 2). |
| [`agent/crates/mesh-agent-core/src/update.rs`](../../agent/crates/mesh-agent-core/src/update.rs) (tests module) | 4 new tests for the precheck (Part 3). |
| `scripts/tests/release-agent-gate.test.sh` (new) | 3 bash tests for the workflow gate's shell logic (Part 3). |
| [`docs/Agent-Updates.md`](../../docs/Agent-Updates.md) | One paragraph noting the precheck behavior (skip on hash match) so operators understand "Update ack: already up to date" with the new reason. |
| [`.claude/phases.md`](../phases.md) | Completed-entry row. |

**No ADR.** This is a workflow + small code change inside an existing accepted ADR (ADR-005 Agent Auto-Update). The decision being recorded is "the existing ADR's contract is preserved, with a precheck added for efficiency" — not a new architectural direction.

### Out of scope (explicit, deferred)

- **Path-gate `build-image.yml`** (server container image). Different artifact, different consumers (CD pulls images by tag for deploy), different blast radius. Worth doing as a follow-up after this pattern lands. Note in [phases.md](../phases.md) Planned table.
- **Determinize the agent build** (`[profile.release]` pin, `SOURCE_DATE_EPOCH`, `--locked`). Would enable content-hash equivalence assertions across rebuilds, but the precheck above (Part 2) already covers the cost it would unlock. Defer.
- **Auto-publish manifest in `release-agent.yml`** (close the manual-step gap). The fix in this plan is a prerequisite — without the gate, auto-publish would push identical binaries to the fleet on every server-only feat:. Track separately; revisit after this plan lands.
- **Split tag namespaces** (`agent-vX.Y.Z` vs `vX.Y.Z`). Would require coordinated changes in `build-image.yml` (which currently keys off `v*`), `auto-tag` (which currently produces a single tag), `install.sh` (which currently queries `/releases/latest`), and CHANGELOG categorization. Larger one-time refactor with similar end-state cleanliness to the path-gate approach. Defer; reconsider only if cross-artifact friction emerges.

### Verification

End-to-end check after merge:

1. **Negative path (docs-only fix):**
   - Land a PR with title `fix(docs): typo`.
   - `auto-tag` bumps `vX.Y.Z` → `vX.Y.(Z+1)`, pushes tag.
   - `release-agent.yml` `check-agent-changed` outputs `agent_changed=false`, `build` skips, `release` skips.
   - `gh release view vX.Y.(Z+1)` returns "release not found".
   - `gh release list --limit 1` still returns the previous binaries-bearing release.
   - `curl -sf https://api.github.com/repos/volchanskyi/opengate/releases/latest | jq -r .tag_name` returns the previous tag.

2. **Positive path (agent fix):**
   - Land a PR touching `agent/crates/mesh-agent/src/main.rs` with title `fix(agent): some-change`.
   - `release-agent.yml` `check-agent-changed` outputs `agent_changed=true`.
   - `build` runs both matrix targets; `release` publishes binaries.
   - `curl -sf .../releases/latest` returns the new tag with both binary assets attached.

3. **Manual re-dispatch (idempotency):**
   - Go to Actions → Release Agent → Run workflow → enter the docs-only tag from step 1.
   - Gate outputs `agent_changed=false`; build skips. Same answer as auto-dispatch.

4. **Agent precheck:**
   - Local: run the new Rust tests — `cd agent && cargo test -p mesh-agent-core apply_update`.
   - End-to-end: stage a manifest entry whose `sha256` matches the running agent's binary (compute via `sha256sum /opt/opengate/mesh-agent`); send via control message; agent should log "current binary already matches manifest hash, skipping download" and send `UpdateAck { success: true, reason: "already up to date" }` without invoking the watchdog.

5. **Bash gate tests:**
   - `bash scripts/tests/release-agent-gate.test.sh` returns exit 0 and prints `3/3 passed`.
