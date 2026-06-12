# W3 — Strict-Mode Classification and Sourced-Library De-Leak

**Parent:** [`shell-quality-hardening.md`](shell-quality-hardening.md) · Commit 3
of 7.

## Goal

Make every script's execution semantics explicit and machine-checked, and stop
the two sourced libraries from mutating their caller's shell options.

## Classification model

Every tracked `.sh` is exactly one of:

1. **Standalone fail-fast executable** — `set -euo pipefail` (add `-E` only with
   an inherited `ERR` trap); expected non-zero statuses handled explicitly.
2. **Failure-aggregating executable / test** — `set -uo pipefail`; captures and
   asserts every expected status; **listed with a reason in the exception
   manifest**. Canonical member:
   [`precommit-gauntlet.sh`](../../../scripts/precommit-gauntlet.sh) (38
   `run_check` steps that must survive a failed check to report the full set).
3. **Sourced library** — no `set`/`shopt`/`trap` at file scope; returns statuses
   rather than exiting unless its contract says otherwise.

## Empirical targets (verified 2026-06-11)

Two sourced libraries currently mutate caller state and must be de-leaked:

- [`.claude/hooks/lib/common.sh`](../../hooks/lib/common.sh) — `set -euo pipefail`
  at file scope **and** `trap _fail_closed_handler ERR`. Its hook callers
  already establish their own fail-closed policy; move the `set`/`trap` into the
  callers (or a guarded init function they opt into) so sourcing the lib is
  side-effect-free.
- [`deploy/scripts/common.sh`](../../../deploy/scripts/common.sh) — `set -euo
  pipefail` at file scope; the deploy scripts that source it already set their
  own options.

The other two libs ([`scripts/lib/loki-push.sh`](../../../scripts/lib/loki-push.sh),
[`scripts/lib/postgres-prereq.sh`](../../../scripts/lib/postgres-prereq.sh)) are
already clean — leave them and use them as the reference contract.

## File inventory

**New:**

- `scripts/check-shell-policy.sh` — the classifier/checker. Rejects: missing or
  non-Bash shebang; a script whose class is unset; a stale/unused exception
  entry; a sourced library that calls `set`/`shopt`/`trap` at file scope; a
  standalone script lacking the required strict declaration; an unapproved
  `set +e`/broad option-disable. Enumerates via `git ls-files`.
- `.claude/shell-policy.exceptions` (or `scripts/shell-policy.exceptions`) — the
  manifest: one row per aggregator/exception with file + reason. Stale-entry
  detection means a deleted/renamed file fails the checker.
- `scripts/tests/check-shell-policy.test.sh` — decision-table tests: a clean
  standalone passes; a library with file-scope `set` fails; an aggregator absent
  from the manifest fails; a manifest entry for a nonexistent file fails; a bad
  shebang fails. **Fails before** the checker exists.

**Modified:**

- `.claude/hooks/lib/common.sh`, `deploy/scripts/common.sh` — remove file-scope
  option/trap mutation; relocate into callers or an explicit opt-in init.
- Affected callers of those two libs — add the `set`/`trap` they previously
  inherited, so behavior is unchanged.
- The one `set -e`-only script (incomplete strict mode) — upgrade to
  `set -euo pipefail` or classify + justify it.
- Strengthen the covering test for any library/caller whose source you touch
  (the repo's pure-refactor TDD rule) before editing it.

## Steps (TDD order)

1. Write `scripts/tests/check-shell-policy.test.sh` (failing) + `chmod +x`.
2. Write `scripts/check-shell-policy.sh` and the exceptions manifest; seed the
   manifest from the current `set -uo pipefail` aggregators (gauntlet, status
   inspectors, the `*.test.sh` harnesses as appropriate).
3. Add/strengthen the covering tests for `common.sh` (hooks + deploy) so the
   de-leak is a verified refactor, not a blind edit.
4. De-leak both `common.sh` files; push the `set`/`trap` into their callers.
   Verify hook fail-closed behavior is preserved (a hook that should block still
   blocks; a sourced helper no longer flips the caller's `errexit`).
5. Upgrade/justify the lone `set -e` script.
6. Run `scripts/check-shell-policy.sh` clean; `make shell-test` green.
7. `/precommit` → commit → `/refactor` → push.

## Risk notes

- **Hook fail-closed regression is the live danger.** `common.sh`'s `ERR` trap
  is what makes the Claude hooks fail *closed*. Moving it must not turn any hook
  fail-*open*. Add an explicit test that a hook sourcing the de-leaked lib still
  exits non-zero on an internal error.

## Out of scope

- Wiring the policy checker into the gauntlet/CI (**W4**).
- Composite-action shell (**W5**); behavioral tests for deploy/installer (**W6**).

## Reviewer checklist

- [ ] `check-shell-policy.sh` enumerates via `git ls-files` and rejects all six
      violation classes (bad shebang, unclassified, stale manifest entry,
      leaking library, missing strict decl, unapproved `set +e`).
- [ ] Exception manifest lists every aggregator with a concrete reason; no
      stale rows.
- [ ] `common.sh` (hooks + deploy) no longer call `set`/`shopt`/`trap` at file
      scope; callers preserve the prior behavior.
- [ ] A test proves a hook sourcing the de-leaked `common.sh` **still fails
      closed** on an internal error.
- [ ] `loki-push.sh` / `postgres-prereq.sh` left untouched (already compliant).
- [ ] `make shell-test` green; `check-shell-policy.sh` clean.
