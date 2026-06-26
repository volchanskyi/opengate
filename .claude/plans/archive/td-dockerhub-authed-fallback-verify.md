# Micro-Plan: Verify Docker Hub Authenticated Fallback

**Register entry:** [techdebt.md](../../techdebt.md) — "Docker Hub authenticated fallback
awaits workflow verification" (removed; pay-down trigger met). **Master:** `techdebt-paydown-master.md`.
**Branch:** `dev`. **Owner:** CI. **Status:** COMPLETE (verified 2026-06-26 — see §9).

## 1. Problem

The shared
[`docker-hub-mirror` action](../../../.github/actions/docker-hub-mirror/action.yml) now
supports authenticated direct Docker Hub fallback, and the protected workflows
([`ci.yml`](../../../.github/workflows/ci.yml),
[`mutation.yml`](../../../.github/workflows/mutation.yml),
[`e2e-cross-browser.yml`](../../../.github/workflows/e2e-cross-browser.yml)) pass the
optional `DOCKERHUB_USERNAME`/`DOCKERHUB_TOKEN`. The login skips when creds are absent
(so forks still run). **Unverified:** that on a protected run the authed login actually
succeeds **and** neither credential is printed.

## 2. Scope

**In:** confirm, from a real protected-workflow run, (a) the authenticated-login
success path executes when the mirror falls back, and (b) the username/token are
masked (never echoed). Add a clear, non-secret success log line if the action lacks one.
**Out:** changing the mirror/fallback logic; fork behavior.

## 3. File inventory

| File | Change |
|---|---|
| `.github/actions/docker-hub-mirror/action.yml` | Only if needed: emit a non-secret success line (e.g. `authenticated Docker Hub login OK`); ensure `::add-mask::` on inputs before any use. |
| `scripts/tests/…` (shell behavioral test) | If the action gains/changes shell logic, add a deterministic behavioral test per the shell-quality rules. |

## 4. Approach

1. Inspect the action's login step: confirm inputs are masked and a success line is
   emitted only on actual login (not on skip).
2. Trigger a protected run (`workflow_dispatch` on `dev`) that exercises the fallback;
   or inspect the most recent protected run's logs.
3. Grep the run logs: success line present on the authed path; **no** raw token/username
   value anywhere (masked as `***`).
4. If the action lacks a clear success line or masking, add it (shell change →
   `make shell-check` + a behavioral test), then re-run.
5. `/precommit` → commit → `/refactor` → `/precommit` → commit → push (only if files changed).

## 5. Quality metrics / acceptance

- [x] A protected-workflow run log shows authenticated-login success on the fallback path.
- [x] Neither `DOCKERHUB_USERNAME` nor `DOCKERHUB_TOKEN` appears unmasked in any log.
- [x] Fork PRs still run (login cleanly skips without creds).
- [x] If code changed: `make shell-quality` + `/precommit` green. — N/A, no code change required.

## 6. NFRs

- **Security:** the central concern — no secret leakage in CI logs; masking verified.
- **Reliability:** fallback prevents Docker Hub rate-limit failures on protected runs.

## 7. Reviewer checklist

- [x] Evidence (log excerpt, secrets redacted) attached — see §9.
- [x] Success line fires only on real login, not on the skip branch.
- [x] No secret value interpolated into an `echo`/log.

## 8. Risks

- Cannot be fully verified locally — requires a real protected run with the repo secrets.
- If the mirror rarely falls back, may need a forced-fallback test path to observe it.

## 9. Verification result — COMPLETE (2026-06-26)

Verified empirically against real CI runs. **No code change required** — the action
already emits a non-secret success line, gates the login on token presence, passes the
token via `--password-stdin`, and relies on GitHub's automatic masking of
`${{ secrets.* }}` (proven active below). The login sub-step runs on every protected
run when a token is present (it is not contingent on an actual mirror miss), so no
forced-fallback path was needed.

- **Authed-login success on a protected run.** CI run 28261771167 (push → `dev`),
  jobs *Go Unit Tests* and *E2E Tests*: `Login Succeeded` followed by
  `authenticated to Docker Hub; fallback pulls bypass the anonymous limit`. The success
  line is emitted only after a real `docker login` (the script runs under `set -e`).
- **No secret leakage.** In that run both credentials render only as `***` everywhere
  they appear (`with: dockerhub-username`/`dockerhub-token`, and `env: DH_USER`/`DH_TOKEN`);
  the login output is just `Login Succeeded` + the standard docker credential-store
  warning + the success line — no raw username or token. GitHub's secret masking is
  active and sufficient, so an explicit `::add-mask::` would be redundant.
- **Fork/skip path still runs.** Dependabot PR run 28248423295 (faithful proxy —
  its `secrets.*` resolve to empty): the composite `with:` block is empty, the `login`
  sub-step is absent, `configure` still ran (`docker up with registry mirror`), and the
  job passed. The `if: inputs.dockerhub-token != ''` gate skips login cleanly when
  credentials are unavailable.

Register entry removed (pay-down trigger met); the git history + this archived plan are
the record.
