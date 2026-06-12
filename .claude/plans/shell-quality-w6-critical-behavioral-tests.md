# W6 — Critical Behavioral Tests

**Parent:** [`shell-quality-hardening.md`](shell-quality-hardening.md) · Commit 6
of 7. **Highest-churn, highest-care commit.** Governed by Guardrail B in the
master plan.

## Goal

Give the highest-risk Shell-owned behavior — installer, bastion, deploy paths,
observability transports — deterministic, offline tests that exercise **real**
script logic (branching, cleanup, idempotency, secret-redaction), not stubs
asserting their own invocation.

## Guardrail B (non-negotiable)

A stub on `PATH` (`curl`, `systemctl`, `oci`, `ssh`, `docker`, `kubectl`,
`install`) exists to let the script **run its real control flow offline**. Tests
assert observable side-effects — files created/removed in a `t`-style
`mktemp -d`, exit codes, redaction of secrets in output, idempotent re-runs —
**never** merely "the stub was called." This matches the
[test-determinism rule](../rules/tests-determinism.md): every test always runs,
deterministically, with no live infra and no silent skip. Any script where a
meaningful offline assertion is impossible without live infra is **flagged here
for a real dependency seam (small DI) rather than given a fake-green test** — do
not lower the bar to hit a checkbox.

## Risk tiers and required coverage

| Tier | Scripts | Required cases |
|---|---|---|
| Critical | [`install.sh`](../../server/internal/api/install.sh), [`bastion-session.sh`](../../deploy/scripts/bastion-session.sh), [`deploy.sh`](../../deploy/scripts/deploy.sh), [`rollback.sh`](../../deploy/scripts/rollback.sh) | success, malformed input, dependency failure, cleanup, idempotency, secret-redaction |
| Gate/parser | smoke/wait helpers, summarizers already partially covered | decision-table + malformed fixture |
| Transport | [`pmat-loki-push.sh`](../../scripts/pmat-loki-push.sh), [`mutation-loki-push.sh`](../../scripts/mutation-loki-push.sh), [`terraform-drift-loki-push.sh`](../../scripts/terraform-drift-loki-push.sh), `pmat-loki-query.sh` | transport selection (ssh vs kubectl vs curl), cleanup, redaction |

## File inventory

**New (each `chmod +x`, fails before the seam/test exists):**

- `scripts/tests/install-sh.test.sh` — installer with stubbed `curl`,
  `systemctl`, `install`, checksum tools; assert: clean install creates expected
  files, checksum-mismatch aborts and cleans up, re-run is idempotent, no secret
  in trace output. **Runs without root, network, or systemd.**
- `scripts/tests/bastion-session.test.sh` — stub `oci`/`ssh`; cache fixtures;
  assert session reuse vs fresh creation, and **cleanup on exit** (temp/session
  artifacts removed even on failure path).
- `scripts/tests/deploy-rollback.test.sh` — fake Docker/Compose state; assert
  deploy failure triggers rollback, rollback restores prior tag, idempotent
  re-deploy.
- `scripts/tests/loki-transport.test.sh` — stub `ssh`/`kubectl`/`curl`; assert
  correct transport is selected per environment and temp files are cleaned.

**Possibly modified (only the seams tests require):**

- The above production scripts — introduce minimal dependency-injection seams
  (e.g. overridable `CURL=${CURL:-curl}`, a redaction helper) **only where
  needed** for deterministic testing. Strengthen the covering assertion before
  each such edit (TDD pure-refactor rule).

## Steps (TDD order)

1. For each target: write the failing test first (stubs + side-effect
   assertions).
2. Add the **minimal** seam to the script if (and only if) the test cannot
   assert real behavior without it; record why in the test header.
3. Make the test pass; confirm it runs with zero network/root/systemd/Docker.
4. Run `make shell-test` (auto-discovers the new `*.test.sh`).
5. For any script where offline testing proves infeasible, write a short note in
   the test file + the W7 docs flagging it for a real seam, rather than a stub
   green. Surface it in the PR description.
6. `/precommit` → commit → `/refactor` → push.

## Reviewer checklist (apply Guardrail B strictly)

- [ ] Every new test asserts **observable side-effects / exit paths**, not
      "stub was invoked." Reject any assertion that only checks the stub ran.
- [ ] Tests run with **no** live OCI/kube/Docker/systemd/network and **no**
      `t.Skip`-equivalent early return; they always execute (test-determinism
      rule).
- [ ] Installer test covers checksum-mismatch abort **and** cleanup **and**
      idempotency **and** secret-redaction.
- [ ] Bastion test asserts cleanup on the failure path, not just the happy path.
- [ ] Deploy/rollback test asserts rollback actually restores prior state.
- [ ] Seams added to production scripts are minimal and covered; no behavior
      change beyond the injection point.
- [ ] Any script that couldn't be tested offline is **flagged for a real seam**,
      not papered over with a fake green.
