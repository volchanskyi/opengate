# Micro-plan: Download SPOF Hardening (single-mirror concern)

**Status:** Proposed — awaiting review/approval before implementation.
**Owner:** delegated engineer. **Reviewer:** Ivan (verifies against the acceptance checklist).
**Branch:** `dev` (all work; no feature branches — see [`.claude/rules/git.md`](../rules/git.md)).

---

## 1. Why this exists

Commit `473d7fa` ("ci: route Docker Hub pulls through a pull-through mirror")
fixed intermittent CI failures pulling `postgres`/scanner/base images from
`registry-1.docker.io` (anonymous, shared-runner-IP rate limits + connection
timeouts) by pointing the Docker daemon at Google's public pull-through cache:

```json
{"registry-mirrors":["https://mirror.gcr.io"]}
```

That **traded one single point of failure for another**. The job set now depends
on `mirror.gcr.io` being reachable and not rate-limiting. Two residual gaps:

1. **The mirror itself is a SPOF.** If `mirror.gcr.io` is down or throttles, the
   daemon falls back to `registry-1.docker.io` — **anonymously**, i.e. straight
   back into the exact rate limit the change was meant to dodge. The "fallback"
   is not an independent second source.
2. **The config is duplicated in three places** that can silently drift apart.
   Improving the mirror story in one place and forgetting the other two has
   already happened once (the inline copies were added in the same commit).

This micro-plan removes both gaps: **one source of truth** for the daemon
config, and a **genuinely independent fallback** (authenticated Docker Hub
pulls) so a mirror outage degrades gracefully instead of failing.

> Honest scoping note: there is **no second public, anonymous Docker Hub
> pull-through cache** that is as reliable as `mirror.gcr.io`. "Add a second
> mirror" is therefore *not* the fix. The fix is to make the **fallback path**
> robust (authenticated → not anonymous-rate-limited) and to keep the config
> in one auditable place. `registry-mirrors` stays a list, so a future
> self-hosted/alternative cache can be appended without re-architecting.

---

## 2. Current state — exact inventory

### 2.1 The three copies of the daemon config

| # | Location | Why it's there | De-dup target |
|---|----------|----------------|---------------|
| A | [`.github/actions/docker-hub-mirror/action.yml`](../../.github/actions/docker-hub-mirror/action.yml) | The composite — canonical | **Becomes the single source of truth** |
| B | [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml), step "Start Postgres with max_connections=400" | Runs **before** `actions/checkout@v6`, so a local-composite `uses:` is not available *at that position* | Reorder checkout earlier, then `uses:` the composite |
| C | [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml), step "SonarCloud Scan (Docker fallback on CDN failure)" | Inlined for locality inside a conditional `run:` step | Replace with a `uses:` step carrying the same `if:` |

### 2.2 The five sites that consume the composite (already correct — do not regress)

- `.github/workflows/ci.yml` — three `- uses: ./.github/actions/docker-hub-mirror`
  steps (the two `pg-ci` legs and the E2E "Start test server" job).
- `.github/workflows/e2e-cross-browser.yml` — one use.
- `.github/workflows/e2e-multiserver.yml` — one use.

> Verify the live line numbers with
> `grep -rn "docker-hub-mirror\|registry-mirrors" .github/` before editing —
> do not trust the numbers in this plan, they drift.

---

## 3. Decision required before implementation

**Add authenticated Docker Hub pulls?** This is the core of the hardening and it
needs a repo secret the reviewer must provision.

- **Recommended (do it):** create a Docker Hub account + a **read-only** Access
  Token (hub.docker.com → Account Settings → Personal access tokens), store as
  repo secrets `DOCKERHUB_USERNAME` + `DOCKERHUB_TOKEN`. Authenticated pulls lift
  the anonymous limit, making the Docker Hub fallback a true independent source.
  The login step is written to **no-op cleanly when the secret is absent** (forks
  / PRs without secret access still run, just unauthenticated), so it never hard-
  fails CI.
- **Alternative (decline the secret):** ship only §4.1 (de-dup) + §4.4 (guard).
  The SPOF is *reduced* (single auditable config, easy to extend) but not *fixed*
  — the fallback stays anonymous. The plan is structured so the auth layer (§4.2)
  is cleanly separable and can be added later without rework.

This is the one open question. Everything else has a clear default below.

---

## 4. Implementation steps

> **TDD order matters** — the repo's hooks block a source edit until a test
> change exists on the branch (see [`.claude/rules/tdd.md`](../rules/tdd.md)).
> Write the test in §4.4 **first** (it will fail), then make the changes.

### 4.0 (FIRST) Author the single-source-of-truth test — see §4.4

Write `scripts/tests/docker-hub-mirror.test.sh` before touching any YAML. It will
fail until §4.1–4.3 land. This satisfies the TDD gate and doubles as the
permanent regression guard.

### 4.1 Collapse to one source of truth

Make `.github/actions/docker-hub-mirror/action.yml` the **only** place a
`registry-mirrors` literal appears.

- **Site C (ci.yml sonar fallback):** the job has already checked out by then.
  Replace the three inline daemon-config lines (`mkdir`/`tee daemon.json`/
  `systemctl restart docker` + the readiness loop) with a dedicated step placed
  immediately before the scanner pull:
  ```yaml
  - uses: ./.github/actions/docker-hub-mirror
    if: env.SONAR_TOKEN != '' && steps.sonar_action.outcome == 'failure'
  ```
  Keep the existing retry-pull loop for `sonarsource/sonar-scanner-cli`.
- **Site B (mutation.yml pg-ci):** move `- uses: actions/checkout@v6` **above**
  the "Start Postgres" step (checkout has no Docker Hub dependency, so this is
  safe), then insert `- uses: ./.github/actions/docker-hub-mirror` between
  checkout and the `docker run ... postgres:17-alpine` line. Delete the inline
  daemon-config block from that step.
  - If reordering checkout turns out to break a later assumption in that job
    (it should not — confirm by reading the whole `pg-ci` leg), fall back to
    keeping the block inline **but** have the §4.4 test assert it is byte-for-byte
    the canonical snippet. Prefer the `uses:` form; only fall back with a one-line
    note in the step comment explaining why.

After this step, `grep -rn "registry-mirrors" .github/` must return **exactly one
line** — inside the composite.

### 4.2 Add an authenticated fallback layer (gated on the secret — see §3)

In `.github/actions/docker-hub-mirror/action.yml`, add inputs and a login step
that runs only when credentials are supplied:

```yaml
inputs:
  dockerhub-username:
    description: Docker Hub username for authenticated fallback pulls. Optional.
    required: false
    default: ""
  dockerhub-token:
    description: Docker Hub read-only access token. Optional.
    required: false
    default: ""
runs:
  using: composite
  steps:
    - name: Configure registry mirror + restart Docker
      shell: bash
      run: |
        # ...existing daemon.json + restart + readiness loop, unchanged...
    - name: Authenticated Docker Hub login (skipped when no token)
      if: inputs.dockerhub-token != ''
      shell: bash
      env:
        DH_USER: ${{ inputs.dockerhub-username }}
        DH_TOKEN: ${{ inputs.dockerhub-token }}
      run: |
        set -euo pipefail
        echo "$DH_TOKEN" | docker login -u "$DH_USER" --password-stdin
        echo "authenticated to Docker Hub — fallback pulls bypass the anonymous limit"
```

Then pass the secrets at **every** `uses:` site (the 5 existing + the 2 converted
in §4.1), e.g.:

```yaml
- uses: ./.github/actions/docker-hub-mirror
  with:
    dockerhub-username: ${{ secrets.DOCKERHUB_USERNAME }}
    dockerhub-token: ${{ secrets.DOCKERHUB_TOKEN }}
```

> Why login *and* mirror: the mirror serves cached common images fast; on a
> mirror miss **or** mirror outage the daemon falls back to Docker Hub, and the
> login makes that fallback authenticated (independent second source). Defense
> in depth — this is what actually retires the SPOF.

### 4.3 Leave `registry-mirrors` as a list (no change, just confirm)

Keep the JSON value an array. Document in the composite's `description` that
additional caches can be appended. **Do not** invent a second public mirror —
none is reliable enough to depend on (see §1 note).

### 4.4 Reintroduction / single-source-of-truth guard (the TDD artifact from §4.0)

Create `scripts/tests/docker-hub-mirror.test.sh`, modeled on the existing
[`scripts/tests/build-image-workflow.test.sh`](../../scripts/tests/build-image-workflow.test.sh)
(same pass/fail harness, `set -euo pipefail`, exit non-zero on any failure). It
is auto-discovered by the gauntlet's `scripts/tests/*.test.sh` loop — no gauntlet
edit needed. Make it `chmod +x`. Assert:

1. **Single source of truth:** `registry-mirrors` appears in **exactly one** file
   under `.github/`, and that file is `.github/actions/docker-hub-mirror/action.yml`.
   (Catches any future inline reintroduction or drift.)
2. **All Docker-Hub-pulling jobs route through the composite:** for each workflow
   that pulls a `docker.io` image (grep for `docker run`/`docker compose`/
   `docker pull`/`postgres:` service usage), assert the job either `uses:` the
   composite or — for the one pre-checkout exception, if §4.1's fallback path was
   taken — contains the canonical config. Encode the allowed exception explicitly
   so a *new* bare pull fails the test.
3. **(If §4.2 shipped)** the composite has the optional `dockerhub-token` input
   and a login step gated `if: inputs.dockerhub-token != ''`.

Keep the assertions grep-based and scoped to `.github/` + `scripts/` only (never
`docs/`/ADRs — they legitimately describe history).

---

## 5. Reviewer acceptance checklist

- [ ] `grep -rn "registry-mirrors" .github/` returns **exactly one** line (inside
      the composite). No inline copies remain in `mutation.yml` or `ci.yml`.
- [ ] mutation.yml pg-ci and ci.yml sonar-fallback both obtain the mirror via the
      composite (or the documented pre-checkout exception, with the test covering it).
- [ ] All 7 `uses:` sites (5 existing + 2 converted) pass the Docker Hub creds
      (if §4.2 shipped) and the login step is gated on token presence.
- [ ] `scripts/tests/docker-hub-mirror.test.sh` exists, is executable, fails if a
      bare/duplicated `registry-mirrors` literal is reintroduced, and passes on the
      final tree.
- [ ] `actionlint` clean (the gauntlet runs it); `make lint` green.
- [ ] If §4.2 shipped: `DOCKERHUB_USERNAME` + `DOCKERHUB_TOKEN` repo secrets exist
      (reviewer-provisioned) and a CI run shows "authenticated to Docker Hub".
- [ ] No new secret value is committed; token only lives in repo secrets.

---

## 6. Out of scope

- Self-hosting a `registry:2` pull-through cache (reintroduces infra we just
  decommissioned — propose separately if the authenticated fallback proves
  insufficient).
- Touching the Loki/SSH transport — that is the sibling micro-plan
  [`retire-vm-ssh-path.md`](retire-vm-ssh-path.md).
- Pinning image digests / Renovate-style base-image management (orthogonal).

---

## 7. Execution workflow (enforced — no bypass)

Per [`CLAUDE.md`](../../CLAUDE.md) and `.claude/rules/`:

1. `git checkout dev && git pull --rebase origin dev`.
2. Write `scripts/tests/docker-hub-mirror.test.sh` first (§4.0) — this is the
   failing test the TDD gate requires before YAML edits.
3. Make the §4.1–4.3 changes; run `bash scripts/tests/docker-hub-mirror.test.sh`
   and `actionlint` locally until green.
4. `/precommit` (runs the full gauntlet — lints, shell tests, sonar) → must pass.
5. `git commit` (author = Ivan Volchanskyi, **no** `Co-Authored-By`).
6. `/refactor` → `/precommit` again → commit → `git push origin dev`.
7. Report the CI run URL to the reviewer.
