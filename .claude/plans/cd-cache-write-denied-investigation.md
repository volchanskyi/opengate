# CD's Actions cache is read-only — verified cause, locked remedy

**Status:** Decisions locked, ready to implement. **Branch:** `dev`. **Owner:** infra/CI.
**Baseline:** CD run [33150662887](https://github.com/volchanskyi/opengate/actions/runs/33150662887)
— every job green, two cache writes silently denied.

CD's two cache writes are refused and reported as `success`. The deploy-state
cache backing [ADR-025](../../docs/adr/ADR-025-cd-preflight-digest-check.md) has
been dead since 2026-06-30; the rust cache has never once written. The remedy is
to stop asking a workflow that cannot write to keep state at all — read the live
cluster for deploy state, and move the agent build to the workflow that already
owns turning a commit into deployable output.

---

## 1. Established facts

Each verified against primary sources — the Actions API, downloaded run logs,
`git log`, and the cited external pages.

| # | Fact | Evidence |
|---|---|---|
| F1 | Two cache saves are refused, both steps reporting `success`: `##[warning]Failed to save: Unable to reserve cache with key <k>. More details: cache write denied: token has no writable scopes` — for `cd-deploy-state-staging-v1-33150662887` and `v0-rust-aarch64-unknown-linux-musl-deploy-staging-k8s-Linux-x64-0b9fd15e-33584f0b` | Job `98781727209` log, lines 2120 and 2264; step conclusions 22 and 50 both `success` |
| F2 | Neither key exists — across all **112** cache entries | `gh api …/actions/caches --paginate` |
| F3 | **Reads work.** `Cache hit for: node-cache-…` / `Cache restored successfully` in the same job | Same log, lines 157–163 |
| F4 | The regression window is **2026-06-29T03:26Z → 2026-06-30T00:42Z**. Last write: run `28346668168`. First denial: run `28412361007`. | Log bisection over retained CD runs |
| F5 | **Not caused by anything in this repo.** No commit touches `cd.yml` inside the window (nearest 2026-06-25 and 2026-07-01), and `actions/cache/save@v6` wrote successfully on 2026-06-29 — so the `@v5→@v6` bump is not the cause either. | `git log`; run `28346668168` log |
| F6 | **build-image writes caches today, through the same client.** Run `33085184229` (event `workflow_run`, ref `refs/heads/main`) logs `Cache saved with key: cache-trivy-2026-08-27` — `actions/cache`, via `trivy-action` — plus `importing cache manifest from gha:…` and `preparing build cache for export … 13.7s done`. | Run log |
| F7 | The cascade is CI `33150133646` (`push`, dev) → build-image `33150632730` (`workflow_run`, main) → CD `33150662887` (`workflow_run`, main). build-image's upstream event is `push`; CD's is `workflow_run`. | Runs API |
| F8 | **build-image and CD check out the same commit** — both `head_sha=537f47900dada1d96cddfb52e1f32e5f9e224b50`. CI's is `a30eb4f8` on dev, a different commit. | Runs API |
| F9 | The ADR-025 skip **has executed** — 33 times in June, skipping production every time. Census of all 102 June CD runs: 38 full deploy, **33 resolve=success / staging=skipped / prod=skipped**, 22 upstream-gated, 8 no-jobs, 1 staging failure. Run `28258968870` logs `##[notice]Pre-flight: digest and deploy configuration unchanged → skip staging` with a restored prior digest. | Jobs API + run logs |
| F10 | Measured skip hit rate: **33 of 72** runs that reached a pre-flight decision = **45.8%**, against a ~14-minute run. | Same census |
| F11 | The rust cache was added **2026-08-27** (`93630d4c`), two months after the restriction — it has never written. The June staging job (`83972031107`) has no rust-cache step and no agent build at all; it ran 2m26s. | `git log -S`; jobs API |
| F12 | Cold cost in the baseline run: `Install cross tools` 38s, `Build the agent…` 3m13s, rust-cache restore 0s (`No cache found.`). `Deploy staging` 7m33s; whole run ~14m. | Job `98781727209` steps |
| F13 | Artifacts are unaffected — `Artifact fault-evidence-pod-delete has been successfully uploaded!` in the same run. | Run log |
| F14 | **Cross-run artifact download is supported and documented**: `actions/download-artifact@v8` with `run-id` + `github-token`, requiring `actions: read`. CD already holds `actions: read` at [`cd.yml:26`](../../.github/workflows/cd.yml#L26); the repo pins v8 in five workflows. | actions/download-artifact docs; repo grep |
| F15 | A failing build-image already stops CD entirely — `resolve-tag` requires `github.event.workflow_run.conclusion == 'success'` ([`cd.yml:34-36`](../../.github/workflows/cd.yml#L34-L36)). | Workflow source; the 22 upstream-gated June runs |
| F16 | Production carries `required_reviewers`, so ADR-025's stated reason for treating it differently is real. | `GET …/environments/production` |
| F17 | The last 8 CD runs are 6 failure / 1 cancelled / 1 success, failing in `Release the staging namespace`, `Enrol two machines`, `Playwright E2E` (×3) and `Helm upgrade`. **None** failed in the agent build; cold cache contributed to none of them. | Jobs API |

## 2. Disproven — do not re-derive

| Claim | Verdict |
|---|---|
| "The cache client is the discriminator; buildx `type=gha` may be silently dead" | **False.** F6 — build-image writes through `actions/cache`, the same client, and its buildx export is demonstrably live. |
| "The `permissions:` block is the discriminator" | **False.** release-agent's `build` job holds **only** `contents: read` and writes on `refs/heads/main`; CD is denied while holding `issues: write`. cd.yml declares no job-level `permissions:` at all. The message is about the cache token, not `GITHUB_TOKEN`. |
| "Branch scope is the discriminator" | **False.** Writer and denied job both run on `refs/heads/main`; for `workflow_run`, `github.ref` is always the default branch. |
| "`should_skip_staging=true` has never executed / would reach production untested" | **False.** F9 — 33 executions, production skipped every time. |
| "The skip may fire too rarely to be worth keeping" | **False.** F10 — 45.8%. |
| "A static YAML scan can guard cache writes" | **False.** Three of build-image's four cache writes happen inside `trivy-action` and `setup-qemu-action`, which declare nothing. |
| The 2026-06-26 changelog explains CD's denial | **It does not.** It scopes the change to `pull_request_target`, `issue_comment` and *fork-pull-request* `workflow_run` cascades, conditioned on *"someone other than a repository collaborator can trigger the event"*. By its own wording CD should be unaffected. The cited heroku issue concerns `pull_request`, mentions neither `workflow_run` nor `actions: write`. |

**Still open:** whether CD is caught by cascade depth or by its upstream running
on the default branch. Both coincide here. Settled by §4 PR 1.

## 3. Locked decisions

| # | Decision |
|---|---|
| D1 | **Agent binary moves to [`build-image.yml`](../../.github/workflows/build-image.yml)** — a matrix job builds both musl targets with `rust-cache` and uploads them as artifacts. It is the only trusted upstream workflow that checks out the commit CD deploys (F8), and its cache token writes (F6). |
| D2 | **Artifact retention: 20 days**, matching the SBOM and release-agent convention. |
| D3 | **On `workflow_dispatch`, CD resolves the build-image run whose `head_sha` matches the dispatched tag's 7 characters** and downloads its artifact. If that artifact has aged out, CD **fails loudly**, naming the tag and the run. No second build path anywhere in the repo. |
| D4 | **Deploy state comes from the live cluster.** `resolve-tag` runs `oci-kube-setup` (~36s, measured) and reads the running image tag off the staging Deployment; the `sha-<7>` tag yields both digest and commit for the `deploy/**` diff. The Actions cache leaves the pre-flight entirely, and with it ADR-025's stale-cache edge case. |
| D5 | **The production skip stays.** It has run 33 times and is defensible on an identical digest with unchanged `deploy/**`. ADR-025 is corrected to describe the over-determined gate actually implemented. |
| D6 | **Run the one-arm probe** to settle cascade depth vs upstream branch. |
| D7 | **Guard: in-run read-back.** After a cache write, assert via the cache-list API that the key exists; fail the job if not. No dependence on warning text, and it uses `actions: read`, which CD already holds. |
| D8 | **Rule: a new [`.claude/rules/ci-cd-determinism.md`](../rules/ci-cd-determinism.md)**, indexed in [`CLAUDE.md`](../../CLAUDE.md). |

### Why the production skip is over-determined

ADR-025 says `deploy-production-k8s` "does not use the staging skip" and lists
production pre-flight Out Of Scope. Both are wrong. Production is gated twice:
`should_skip_staging != 'true'` at [`cd.yml:492`](../../.github/workflows/cd.yml#L492)
**and** `needs.deploy-staging-k8s.result == 'success'` — and a skipped job returns
`skipped`. Deleting the first clause would change nothing. The ADR describes a
design the workflow has never implemented.

---

## 4. Implementation

### PR 1 — the probe

Splits the last open question. Two throwaway workflows on `dev`:

1. `probe-upstream.yml` — `on: push: branches: [main]`, one no-op job. Fires
   naturally when `merge-to-main` pushes; **no manual push to `main`.**
2. `probe-cache.yml` — `on: workflow_run: workflows: [probe-upstream], types: [completed], branches: [main]`.
   Writes `cache-scope-probe-${{ github.run_id }}`, then reads it back. Logs
   `github.event.workflow_run.event` to confirm the upstream is `push`.

This arm is depth 1 with upstream **event `push`** on upstream **branch `main`** —
the one combination that separates the two candidates:

- **Writes** → the upstream branch is irrelevant; the upstream *event* is the
  discriminator, and re-triggering CD from a trusted event would fix it at the root.
- **Denied** → the upstream running on the default branch is the discriminator,
  and no trigger change would help.

Either way PRs 2–3 are unaffected; the answer decides only whether a root-level
fix stays on the table.

### PR 2 — probe out

Delete both workflows, delete the probe cache entry, record the result in §2.
Nothing probe-shaped survives on `dev`.

### PR 3 — agent build moves to build-image

1. **`build-image.yml`** — add a `build-agent` job carrying the same `if:` guard
   as `check-image-changed` (so a failed CI does not build) and `permissions: contents: read`.
   Matrix over `x86_64-unknown-linux-musl` and `aarch64-unknown-linux-musl`, checkout
   `ref: main`, `rust-cache`, `cross` for aarch64, `upload-artifact` with
   `retention-days: 20`. **Not gated on `image_changed`** — CD needs the binary on
   the ~80% of runs that take the tag-forward path.
2. **`cd.yml`** — delete the toolchain, musl, `cross`, `rust-cache` and
   `Build the agent for the staging node` steps. Keep the arch resolution, then
   download the artifact for that target: `run-id` from
   `github.event.workflow_run.id`, or from the run resolved per D3 on dispatch.
   Absent artifact fails the job with the tag and run id named.
3. **`ADR-084`** — amend in place; the decision (two real machines built from the
   deployed commit) is unchanged, only where the build happens.
4. **Tests** — extend [`cd-workflow.test.sh`](../../scripts/tests/cd-workflow.test.sh):
   `cd.yml` contains no rust toolchain/cache step, and the download names both
   a `run-id` and a failure path. New build-image assertions for the matrix,
   retention and absent `image_changed` gate.

Failure semantics (F15): an agent that will not compile now fails build-image, so
CD never starts — rather than failing four minutes into the staging job with OCI
credentials on disk and the namespace lease taken. Earlier, and before cluster contact.

### PR 4 — deploy state from the cluster, plus the rule and guard

1. **`cd.yml` `resolve-tag`** — add `oci-kube-setup`; read the staging Deployment's
   running image reference; derive prior digest and prior commit from its `sha-<7>`
   tag; keep the existing `deploy/**` diff and every fail-open branch. Add
   `oci-kube-teardown`. Delete `actions/cache/restore@v6`,
   `Write staging deploy state` and `Cache staging deploy state`.
2. **Guard (D7)** — `scripts/assert-cache-written.sh <key>`: query
   `GET /repos/{repo}/actions/caches`, exit non-zero if the key is absent. Wire it
   after every cache write in workflows we own — including the `setup-node` npm
   cache that remains in CD's staging job, which is subject to the same denial.
   **Test first**, per the TDD gate.
3. **Rule (D8)** — `.claude/rules/ci-cd-determinism.md`: a CI/CD step that warns
   and exits 0 is a false green, the same defect class as
   [`tests-determinism.md`](../rules/tests-determinism.md). Row added to
   `CLAUDE.md`'s table. Enforced by `scripts/tests/ci-cd-determinism.test.sh`
   (new, must be `chmod +x`).
4. **`ADR-086`** — new, superseding ADR-025: the cluster is the source of truth
   for what is deployed, not a cached hint. ADR-025's `status:` updated; row added
   to [`decisions.md`](../decisions.md).
5. **Close-out** — [`phases.md`](../phases.md) Completed row, this plan `git mv`'d
   to `plans/archive/` with links bumped one `../` deeper, references repointed —
   **all in this commit**.

---

## 5. File inventory

| File | Change |
|---|---|
| [`.github/workflows/build-image.yml`](../../.github/workflows/build-image.yml) | new `build-agent` matrix job (PR 3) |
| [`.github/workflows/cd.yml`](../../.github/workflows/cd.yml) | agent build → artifact download; pre-flight reads the cluster; both cache steps deleted (PR 3, 4) |
| `.github/workflows/probe-upstream.yml`, `probe-cache.yml` | added PR 1, deleted PR 2 |
| `scripts/assert-cache-written.sh` | new — the read-back guard (PR 4) |
| [`scripts/tests/cd-workflow.test.sh`](../../scripts/tests/cd-workflow.test.sh) | extended (PR 3) |
| `scripts/tests/ci-cd-determinism.test.sh` | new, `chmod +x` (PR 4) |
| [`.claude/rules/ci-cd-determinism.md`](../rules/ci-cd-determinism.md) | new (PR 4) |
| [`CLAUDE.md`](../../CLAUDE.md) | rule index row (PR 4) |
| [`docs/adr/ADR-025-cd-preflight-digest-check.md`](../../docs/adr/ADR-025-cd-preflight-digest-check.md) | `status:` superseded (PR 4) |
| `docs/adr/ADR-084-staging-e2e-runs-against-real-machines.md` | amended in place (PR 3) |
| `docs/adr/ADR-086-*.md` | new (PR 4) |
| [`.claude/decisions.md`](../decisions.md), [`.claude/phases.md`](../phases.md) | index + ledger rows (PR 4) |

## 6. Reviewer checklist

- [ ] PR 1's probe result recorded in §2; PR 2 leaves no probe workflow and no probe cache entry.
- [ ] `cd.yml` contains no `Swatinem/rust-cache`, no `actions/cache/restore`, no `actions/cache/save`.
- [ ] The agent artifact is downloaded from a run whose commit equals the deployed commit; a missing artifact fails loudly and never silently rebuilds.
- [ ] `build-agent` is **not** gated on `image_changed`, and carries `retention-days: 20`.
- [ ] The pre-flight still fails open on every branch it did before — unresolved digest, unreadable cluster, `deploy/**` changed, `workflow_dispatch`.
- [ ] The read-back guard covers every cache write in workflows we own, including CD's `setup-node` npm cache.
- [ ] Rule + guard land in the same commit as the fix.
- [ ] ADR-025 marked superseded, ADR-086 added with its `decisions.md` row, ADR-084 amended.
- [ ] Plan archived to `plans/archive/` in the completing commit, links bumped, `phases.md` row added.

## 7. Sources

- CD run [33150662887](https://github.com/volchanskyi/opengate/actions/runs/33150662887) (baseline), job `98781727209`
- build-image `33085184229` (writes), release-agent `32972438248` (trusted control), tag-forward `33150632730`
- `28346668168` (last CD write) / `28412361007` (first denial) / `28258968870` (skip firing)
- [Read-only Actions cache for untrusted triggers](https://github.blog/changelog/2026-06-26-read-only-actions-cache-for-untrusted-triggers/) — 2026-06-26
- [actions/download-artifact](https://github.com/actions/download-artifact) — cross-run download contract
