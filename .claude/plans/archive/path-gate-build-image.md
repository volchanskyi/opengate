# Path-gate `build-image.yml` + digest-aware CD redeploy

## Context

Follow-on to `.claude/plans/archive/path-gate-agent-release.md`. That gate stops `release-agent.yml` from rebuilding Rust agent binaries on non-`agent/` commits; today's plan applies the same idea to `build-image.yml` (server container image) with a critical asymmetry that the explorers surfaced.

### The asymmetry vs the agent case

`release-agent.yml` is a leaf — skipping the build means no GitHub Release at the new tag, and `install.sh`'s `/releases/latest` lookup keeps pointing at the most recent binaries-bearing release. CD doesn't care.

`build-image.yml` is **upstream of CD**. CD reads `github.event.workflow_run.head_sha` and pulls `ghcr.io/volchanskyi/opengate-server:sha-<7char>` ([cd.yml:42-51](../../.github/workflows/cd.yml#L42-L51)). The explorer confirmed [cd.yml]/[deploy/scripts/deploy.sh] has **no fallback** — a missing tag fails `docker compose pull` immediately. So "just skip the build" breaks the deploy pipeline.

### What's actually in the image

Per the Dockerfile explorer ([Dockerfile:2-30](../../Dockerfile#L2-L30)):

| Stage | Inputs |
|---|---|
| `web-build` | `web/package.json`, `web/package-lock.json`, `web/**` |
| `server-build` | `server/go.mod`, `server/go.sum`, `server/**` |
| `final` (alpine) | `Dockerfile` itself |

`agent/**` is NOT in the image. `deploy/caddy/**` is NOT in the image (Caddy is a separate container). `api/openapi.yaml` is transitively covered via `server/internal/api/openapi_gen.go ∈ server/**`.

### Frequency

25/30 recent main-branch commits did not touch any image input — 83% of `build-image.yml` runs were rebuilding the same content. Per-build cost is ~1-3 min (BuildKit cache hits keep it low); aggregated waste is ~32 min/30-commit window + ~25 redundant GHCR tags.

### CD vs image build are independent concerns

The explorer also surfaced that CD copies a large infrastructure surface to the VPS on every run ([cd.yml:120-143, 304-322](../../.github/workflows/cd.yml#L120-L143)):

- `deploy/docker-compose*.yml`, `deploy/caddy/**`, `deploy/postgres/init.sql`
- `deploy/victoriametrics/`, `deploy/loki/`, `deploy/promtail/`
- `deploy/grafana/provisioning/**` (datasources, dashboards, alerting)
- `deploy/scripts/*.sh`

So **CD must run for any `deploy/**` change** — that's the only deploy mechanism for the whole infrastructure layer. The earlier "Postgres dashboard datasource UID mismatch" fix (61d2813) is a recent example: pure infra change, no image rebuild needed, but CD absolutely had to run to scp the corrected `postgres.json` to `/opt/opengate/grafana/provisioning/dashboards/`.

This constrains the chosen hybrid option: "skip CD redeploy on identical digest" is correct ONLY when `deploy/**` is ALSO unchanged since the last deploy. Otherwise infra updates silently fail to land.

### Outcome

Two coordinated changes that, together, satisfy the chosen hybrid option:

1. **`build-image.yml` tag-forwards the previous `:latest` digest to the new `sha-<newsha>`** when no image input changed (Part 1). Cost: ~5 s `crane copy`, no rebuild, all cosign/SBOM/Trivy attestations inherited via the digest.
2. **CD compares the pulled digest against the running container's digest AND checks whether `deploy/**` changed since the last successful deploy**; skips `docker compose down/up` only when both are unchanged (Part 2). Tracks "last deployed commit SHA" via a `/opt/opengate/.last-deployed-sha` sentinel file on the VPS.

---

## Implementation

### Part 1 — `build-image.yml` gate + tag-forward

**Baseline ref:** query the current `:latest` image's `org.opencontainers.image.revision` label via `crane manifest`. This is strictly correct (always reflects what's in the registry) and handles `[skip ci]` sequences cleanly. The label is populated automatically by `docker/metadata-action@v6` (no extra workflow code needed; it's been emitting this label for every build).

**Gate logic:** new `check-image-changed` job at workflow entry —

```yaml
jobs:
  check-image-changed:
    name: Check image inputs changed since :latest
    runs-on: ubuntu-latest
    outputs:
      image_changed: ${{ steps.gate.outputs.image_changed }}
      prev_sha:      ${{ steps.gate.outputs.prev_sha }}
    steps:
      - uses: actions/checkout@v6
        with:
          ref: ${{ github.event.workflow_run.head_sha || github.sha }}
          fetch-depth: 0
      - uses: imjasonh/setup-crane@v0.4
      - name: Diff image inputs vs :latest
        id: gate
        run: scripts/build-image-gate.sh
        env:
          IMAGE: ghcr.io/${{ github.repository_owner }}/opengate-server
          HEAD_SHA: ${{ github.event.workflow_run.head_sha || github.sha }}
```

New helper `scripts/build-image-gate.sh` (pure bash, mirrors the structure of `scripts/release-agent-gate.sh`):

- Resolves `prev_sha` via `crane manifest "$IMAGE:latest" | jq -r '.config.digest' | xargs crane config "$IMAGE@" | jq -r '.config.Labels."org.opencontainers.image.revision"'`. Falls back to `agent_changed=true` if `:latest` doesn't exist (first run) or the label is missing.
- Diffs `${prev_sha}...${HEAD_SHA} -- 'server/**' 'web/**' Dockerfile`. Non-empty → `image_changed=true`.
- Emits `image_changed=true|false` + `prev_sha=...` to `$GITHUB_OUTPUT`.

**Build job gating:** existing `build-and-push` job gets `needs: check-image-changed` + `if: needs.check-image-changed.outputs.image_changed == 'true'`. No other change.

**Tag-forward job** (new, runs when build is skipped):

```yaml
  tag-forward:
    needs: check-image-changed
    if: needs.check-image-changed.outputs.image_changed != 'true'
    runs-on: ubuntu-latest
    permissions:
      packages: write
      contents: read
    steps:
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: imjasonh/setup-crane@v0.4
      - name: Tag-forward :latest → :sha-<newsha>
        env:
          IMAGE: ghcr.io/${{ github.repository_owner }}/opengate-server
          HEAD_SHA: ${{ github.event.workflow_run.head_sha || github.sha }}
        run: |
          SHORT_SHA="${HEAD_SHA:0:7}"
          crane copy "$IMAGE:latest" "$IMAGE:sha-${SHORT_SHA}"
          echo "::notice::tag-forwarded :latest → :sha-${SHORT_SHA} (no image inputs changed)"
```

`permissions: packages: write` is scoped to this job only (mirrors the `release: contents: write` pattern we applied last commit).

CD remains unchanged in trigger semantics — `workflow_run` of `Build & Push Container Image` fires on either the build-and-push branch or the tag-forward branch (both succeed → workflow conclusion is "success").

### Part 2 — CD digest-aware redeploy

**On the VPS:** [`deploy/scripts/deploy.sh`](../../deploy/scripts/deploy.sh) sentinel pattern.

After a successful `docker compose pull && down && up -d`, write `$IMAGE_TAG` and the commit SHA into `/opt/opengate/.last-deployed`. Schema:

```
image_tag=sha-abc1234
image_digest=sha256:...
git_sha=abc1234567...
deployed_at=2026-05-19T17:48:05Z
```

On the next CD invocation, before `docker compose down/up`:

```bash
# After `docker compose pull server` has populated the local cache:
NEW_DIGEST=$(docker image inspect "ghcr.io/.../opengate-server:${IMAGE_TAG}" --format '{{index .RepoDigests 0}}' | cut -d@ -f2)
RUNNING_DIGEST=$(jq -r .image_digest /opt/opengate/.last-deployed 2>/dev/null || echo "")
DEPLOYED_SHA=$(jq -r .git_sha /opt/opengate/.last-deployed 2>/dev/null || echo "")

if [[ "$NEW_DIGEST" == "$RUNNING_DIGEST" ]]; then
  # Image is identical. Skip the restart ONLY if no deploy/** changed.
  if [[ -n "$DEPLOYED_SHA" ]] && git -C /opt/opengate/src diff --quiet "$DEPLOYED_SHA" HEAD -- 'deploy/'; then
    echo "Image digest unchanged AND deploy/** unchanged — no-op deploy."
    exit 0
  fi
  echo "Image digest unchanged but deploy/** changed — applying config-only redeploy."
fi
# Otherwise proceed with `docker compose down && up -d`.
```

**Where does `/opt/opengate/src` come from?** Already on the VPS — CD `scp`s the deploy/ tree. We need to also bake in the commit SHA. Easiest: CD writes `$GITHUB_SHA` to a file alongside the deploy.sh invocation. Tracked via the new sentinel.

**Sentinel write happens at the end of deploy.sh after a successful `compose up -d`**, atomically via `mktemp + mv`.

### Critical files

| File | Change |
|---|---|
| [`.github/workflows/build-image.yml`](../../.github/workflows/build-image.yml) | New `check-image-changed` + `tag-forward` jobs; `build-and-push` gains `needs:` + `if:` (Part 1). |
| [`scripts/build-image-gate.sh`](../../scripts/build-image-gate.sh) (new) | Pure bash. Uses `crane manifest` + `crane config` to read `:latest`'s revision label, then `git diff <label-sha>...<HEAD_SHA> -- 'server/**' 'web/**' Dockerfile`. Mirrors structure of [`scripts/release-agent-gate.sh`](../../scripts/release-agent-gate.sh). |
| `scripts/tests/build-image-gate.test.sh` (new) | Mirrors [`scripts/tests/release-agent-gate.test.sh`](../../scripts/tests/release-agent-gate.test.sh) (6 cases): no `:latest` exists, server/ unchanged, server/ changed, web/ changed, Dockerfile changed, deeply-nested file change. Mocks `crane` via a wrapper function so tests don't need network access. |
| [`deploy/scripts/deploy.sh`](../../deploy/scripts/deploy.sh) | Digest-equality gate + sentinel write (Part 2). |
| [`.github/workflows/cd.yml`](../../.github/workflows/cd.yml) | Pass `GIT_SHA=$GITHUB_SHA` env var to deploy.sh so the sentinel records it. |
| [`docs/Continuous-Deployment.md`](../../docs/Continuous-Deployment.md) | One paragraph: "Image-build skip = tag-forward via crane. CD skip = identical digest AND no deploy/** drift." |
| [`.claude/phases.md`](../phases.md) | Completed-entry row. |

No ADR. Same rationale as path-gate-agent-release: this preserves the existing contract (CD always sees a valid image tag, infra changes always deploy), with efficiency added.

### Reused helpers and patterns

- [`scripts/release-agent-gate.sh`](../../scripts/release-agent-gate.sh) — the structural template for `scripts/build-image-gate.sh` (set -euo pipefail, validate inputs, emit `key=value` to stdout, descriptive log lines to stderr).
- [`scripts/tests/release-agent-gate.test.sh`](../../scripts/tests/release-agent-gate.test.sh) — TDD pattern for `scripts/tests/build-image-gate.test.sh` (synthetic git repo, 6 cases, mock fixture for `crane`).
- [`.github/workflows/release-agent.yml`](../../.github/workflows/release-agent.yml) — job-level `permissions:` pattern; per-job `needs:` + `if:` gating.
- `iac-gate` in [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) (lines 1075-1136) — established inline `git diff` idiom for path detection (no `dorny/paths-filter`, no `paths:` filter on triggers).

### Verification

Done locally + in CI after merge:

1. **`./scripts/tests/build-image-gate.test.sh`** — 6/6 pass with mocked crane.
2. **`./scripts/precommit-gauntlet.sh`** — all checks pass; actionlint validates the new workflow jobs.
3. **Negative path (docs-only change on main):** CI passes → build-image.yml fires → `check-image-changed` outputs `false` → `build-and-push` skipped → `tag-forward` runs (~5s) → CD pulls `sha-<newsha>` (identical digest to `:latest`) → digest-equality gate detects no deploy/** changes either → exits 0 without restart. Verify via `docker ps` on the VPS: container uptime > previous deploy.
4. **Image-touching change (`server/internal/api/handlers.go`):** `check-image-changed` outputs `true` → full build + push runs (~1-3 min) → CD pulls, digest differs from running, full redeploy. Verify via `docker ps`: new container, fresh uptime.
5. **Infra-only change (`deploy/grafana/provisioning/dashboards/foo.json`):** `check-image-changed` outputs `false` → tag-forward runs → CD pulls identical digest → digest-equality gate sees deploy/** changed → applies the new compose/grafana config + restarts containers. Verify Grafana picks up the dashboard JSON change.
6. **First-time deploy after this change lands** (no `/opt/opengate/.last-deployed` exists yet): gate fails-open → full redeploy happens → sentinel is written. Next deploy uses the new fast path.

### Risks and mitigations

- **`:latest` was deleted from GHCR.** Gate falls back to `image_changed=true` → forces a full rebuild. Self-healing.
- **`crane manifest` rate-limit / network blip.** `set -e` in the gate fails the whole workflow; safer than skipping a rebuild silently. Workflow re-run from the Actions UI is idempotent.
- **Multiple commits land between CD runs (paused VPS, etc.).** Sentinel records last deployed SHA, so deploy/** diff covers the full gap.
- **Sentinel file corruption / partial write.** `jq -r ... || echo ""` → empty `DEPLOYED_SHA` → diff is empty → falls back to full redeploy. Safe failure mode.
- **GHCR moving `:latest` to a different SHA between gate read and tag-forward.** Tag-forward uses `crane copy "$IMAGE:latest" "$IMAGE:sha-..."` — both sides of the copy resolve through the registry at the same time, so the digest stamped on `sha-` is always the digest `:latest` pointed to at copy time, even if `:latest` moves a microsecond later. No race.

### Out of scope (explicit, deferred)

- **Determinize the server build** (Go `-trimpath` is set; further determinism via `SOURCE_DATE_EPOCH` / locked deps possible but unnecessary now — tag-forward bypasses any non-determinism by reusing the existing digest).
- **Digest-pin every image in compose** (`postgres@sha256:...` etc.). The policy comment in [`policy/docker_compose/images.rego`](../../policy/docker_compose/images.rego) already defers this. Separate plan.
- **Migrate CD to deploy by digest instead of tag** (would eliminate the "missing tag" failure mode entirely). Larger refactor — affects every IMAGE_TAG reference in compose. Separate plan.
- **Notify-only mode** for the gate (instead of skip, print "would skip" and continue). Premature; the path-gate-agent-release pattern proved skipping is safe.
