# Micro-Plan: Verify Docker Hub Authenticated Fallback

**Register entry:** [techdebt.md](../techdebt.md) — "Docker Hub authenticated fallback
awaits workflow verification." **Master:** `techdebt-paydown-master.md`.
**Branch:** `dev`. **Owner:** CI. **Status:** verification (needs a real CI run).

## 1. Problem

The shared
[`docker-hub-mirror` action](../../.github/actions/docker-hub-mirror/action.yml) now
supports authenticated direct Docker Hub fallback, and the protected workflows
([`ci.yml`](../../.github/workflows/ci.yml),
[`mutation.yml`](../../.github/workflows/mutation.yml),
[`e2e-cross-browser.yml`](../../.github/workflows/e2e-cross-browser.yml)) pass the
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

- [ ] A protected-workflow run log shows authenticated-login success on the fallback path.
- [ ] Neither `DOCKERHUB_USERNAME` nor `DOCKERHUB_TOKEN` appears unmasked in any log.
- [ ] Fork PRs still run (login cleanly skips without creds).
- [ ] If code changed: `make shell-quality` + `/precommit` green.

## 6. NFRs

- **Security:** the central concern — no secret leakage in CI logs; masking verified.
- **Reliability:** fallback prevents Docker Hub rate-limit failures on protected runs.

## 7. Reviewer checklist

- [ ] Evidence (log excerpt, secrets redacted) attached to the PR/issue.
- [ ] Success line fires only on real login, not on the skip branch.
- [ ] No secret value interpolated into an `echo`/log.

## 8. Risks

- Cannot be fully verified locally — requires a real protected run with the repo secrets.
- If the mirror rarely falls back, may need a forced-fallback test path to observe it.
