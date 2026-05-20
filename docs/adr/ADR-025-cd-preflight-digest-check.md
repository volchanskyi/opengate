# ADR-025: CD pre-flight digest check to short-circuit no-op staging deploys

Date: 2026-05-20
Status: Accepted

## Context

Commit `76ead5f` ("feat(ci,cd): path-gate build-image.yml + digest-aware CD redeploy") introduced two coordinated savings:

1. **Path-gate in [`build-image.yml`](../../.github/workflows/build-image.yml)** — the `check-image-changed` job runs [`scripts/build-image-gate.sh`](../../scripts/build-image-gate.sh) and skips the expensive multi-arch `build-and-push` job when the diff vs the SHA that produced `:latest` doesn't touch image inputs.
2. **Tag-forward fallback** — when `image_changed=false`, the `tag-forward` job uses `crane copy` to stamp `:latest`'s digest onto the new `sha-<7>` tag. CD requires the `sha-<7>` tag to exist for `docker compose pull`; tag-forward keeps that contract alive without rebuilding.
3. **Digest-aware skip in [`deploy/scripts/common.sh:185-205`](../../deploy/scripts/common.sh#L185-L205)** — `redeploy()` compares the pulled image's digest to the on-host `.last-deployed-staging` / `.last-deployed` sentinel and, when the digest matches AND `deploy/**` didn't change, skips `docker compose down/up`.

This stack saves **the actual container restart** on no-op deploys. It does NOT save the **workflow-level cost** of CD running:

- GitHub's `workflow_run` event has **no `paths` filter** — once `Build & Push Container Image` completes successfully on `main`, [`cd.yml`](../../.github/workflows/cd.yml) is triggered regardless of what changed.
- The `deploy-staging` job ([`cd.yml:82-294`](../../.github/workflows/cd.yml#L82-L294)) runs ~2–3 minutes of setup before reaching the digest-skip in `deploy.sh`: OCI + SSH setup (JIT NSG-rule pattern), `actions/checkout@v6` with `fetch-depth: 0`, `setup-node`, `npm ci`, `playwright install --with-deps chromium`, scp of deploy/ files, ensure-cosign-on-VPS.
- Observed today (run [26140120460](https://github.com/volchanskyi/opengate/actions/runs/26140120460)): a docs-only push triggered a full deploy-staging run that ended in `deploy.sh` logging `"Image digest unchanged AND deploy/** unchanged — skipping compose restart"`. ~5–10 GitHub Actions minutes consumed for zero behavior change.

## Decision

Add a **pre-flight digest check inside the `resolve-tag` job** of `cd.yml`. The check uses `crane digest` against GHCR (no SSH, no OCI) plus a GitHub Actions cache of the prior deploy's state. When the target image digest matches the cached prior digest AND `deploy/**` didn't change since the cached `git_sha`, `resolve-tag` outputs `should_skip_staging=true` and `deploy-staging` is gated off via its `if:` condition.

This is a **best-effort fast path**. The on-VPS sentinel in `deploy.sh` (commit `76ead5f`) remains the source of truth — a stale or missing cache simply falls through to the full deploy, which then redeploys cleanly or skips via its existing logic.

### Design

```
┌─────────────────────────────────────┐
│  resolve-tag (existing job)         │
│  - Determine tag (sha-<7>)          │
│  - Log in to GHCR                   │
│  - Verify image exists              │
│  - Cosign verify                    │
│  - [NEW] Install crane              │
│  - [NEW] Restore cache              │
│  - [NEW] Pre-flight check:          │
│       target_digest = crane digest  │
│       if cached.digest == target    │
│          AND no deploy/** diff      │
│          AND not workflow_dispatch  │
│       then should_skip_staging=true │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│  deploy-staging (existing job)      │
│  if: should_skip_staging != 'true'  │
│  - All existing steps unchanged     │
│  - [NEW] Save fresh cache at end    │
└─────────────────────────────────────┘
```

Cache key: `cd-deploy-state-staging-v1-${{ github.run_id }}`. Restore key prefix: `cd-deploy-state-staging-v1-` (fuzzy match returns the most recent prior entry).

Cached payload (`staging-deploy-state.json`):

```json
{
  "git_sha": "9137516...",
  "image_digest": "sha256:abc...",
  "image_tag": "sha-9137516"
}
```

### Edge cases

| Scenario | Behavior |
|---|---|
| First run after this ADR ships | Cache miss → pre-flight skips its work → `deploy-staging` runs normally → cache populated on success. No-op savings start with the 2nd no-op deploy. |
| `workflow_dispatch` (manual deploy) | Pre-flight ignored — `should_skip_staging` forced to `false`. Manual intent always proceeds. |
| Stale cache (someone deployed manually via SSH) | The cache says "we deployed X" but the VPS is actually at Y. Pre-flight may skip `deploy-staging`. **Risk**: divergence persists until something changes the target tag's digest OR `deploy/**`. **Mitigation**: documented runbook entry — after any manual VPS deploy, run `gh cache delete --key cd-deploy-state-staging-v1-<run_id>` to force the next CD to do a full deploy. The risk is bounded because the cache lifetime is 7 days max (GHA cache eviction). |
| Cache eviction (7 days of no access) | Falls through to full `deploy-staging`. Safe. |
| `crane digest` fails (GHCR transient error) | Pre-flight sets `should_skip_staging=false` and logs a warning. Fail-open to the full deploy path. |
| Image-tag exists in GHCR but is unsigned | The existing cosign-verify step in `resolve-tag` fails BEFORE pre-flight runs. No change in behavior — bad-image deploys still blocked. |
| Production-environment deploys | This ADR covers staging only. Production has its own sentinel (`.last-deployed`), its own `deploy/**` drift check, and a manual approval gate. The savings argument is weaker for production (deploys are intentional). Extending to production is a separate follow-up if approval-queue latency proves to be a real cost. |

### Implementation

`cd.yml` changes:

1. Extend `resolve-tag` checkout to `fetch-depth: 0` (needed for `git diff` against `deploy/**`).
2. Add `imjasonh/setup-crane@v0.4` after the cosign-verify step (same action+pin as `build-image.yml`).
3. Add `actions/cache/restore@v4` step keyed on `cd-deploy-state-staging-v1-`.
4. Add the pre-flight check step that writes `should_skip_staging` to `$GITHUB_OUTPUT`.
5. Add `should_skip_staging` to `resolve-tag`'s `outputs:`.
6. Add `&& needs.resolve-tag.outputs.should_skip_staging != 'true'` to `deploy-staging`'s `if:`.
7. Add a cache-save step at the END of `deploy-staging` (gated by `if: success() && steps.deploy.outputs.deployed == 'true'`).

The full diff lands in the same PR as this ADR.

## Out of scope

- **Production environment pre-flight.** Production has manual approval; the savings calculation is different. Revisit if `deploy-production` queue latency proves to be a recurring cost.
- **Skipping cosign-verify on cache hit.** Cosign-verify takes ~5s and is the last line of defense against tampering. Keep it always-on; not worth the optimization.
- **Skipping `resolve-tag` itself.** It contains the cosign-verify; can't safely skip.
- **Skipping `Build & Push Container Image` (the upstream workflow).** It already has its own path-gate. The pre-flight here is independent of that gate.
- **Cross-workflow state sharing (e.g. using Build & Push's `crane copy` outcome as input).** Adds coupling; the in-process cache is simpler.
- **OCI Object Storage as a more durable state store.** GHA cache is sufficient for the 7-day window; adding an external store is over-engineering.

## Consequences

**Positive.**

- Saves ~2–3 minutes of CD wall-clock per no-op deploy.
- Reduces GitHub Actions minutes spend on the most common deploy shape (auto-merges of docs / CI-only commits).
- The on-VPS sentinel remains the source of truth; the cache is a hint, not a contract. Safety preserved.
- Pre-flight runs on `ubuntu-latest` with no SSH/OCI surface — no new security exposure.

**Accepted trade-offs.**

- One extra step (~10s) on the resolve-tag critical path (cache restore + crane digest + diff). Acceptable in exchange for the no-op savings.
- Stale-cache divergence risk — bounded by 7-day GHA cache lifetime and mitigated by the manual `gh cache delete` runbook entry.
- The cache key versioning (`v1`) means schema changes require a key bump, which forces one full deploy. Acceptable.
- First-deploy-after-ADR pays full cost (cache miss). Acceptable.

## References

- Upstream: commit `76ead5f` (path-gate build-image + digest-aware CD), commit `81a0b61` (HEAD_SHA alignment fix)
- The deploy-side skip: [`deploy/scripts/common.sh:185-205`](../../deploy/scripts/common.sh#L185-L205) (`redeploy()` digest-equality gate)
- The build-side path-gate: [`scripts/build-image-gate.sh`](../../scripts/build-image-gate.sh), [`.github/workflows/build-image.yml`](../../.github/workflows/build-image.yml)
- Workflow being optimized: [`.github/workflows/cd.yml`](../../.github/workflows/cd.yml)
- GitHub Actions cache docs: [actions/cache](https://github.com/actions/cache)
- Observed example: [run 26140120460](https://github.com/volchanskyi/opengate/actions/runs/26140120460) — docs-only push, CD ran end-to-end, deploy was a compose-restart no-op
