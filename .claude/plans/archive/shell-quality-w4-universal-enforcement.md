# W4 — Universal Enforcement (Gauntlet + CI + Changed-File Fast Path)

**Parent:** [`shell-quality-hardening.md`](shell-quality-hardening.md) · Commit 4
of 7.

## Goal

Turn the W1–W3 machinery into hard gates: one runner enforced locally (commit
gauntlet) and in CI, with a fast changed-file path for the agent write/commit
loop, and actionlint pointed at the pinned ShellCheck. **This is the first
commit where shfmt drift and policy violations break a build** — it lands only
*after* the W2 baseline and W3 classifier so neither blocks legitimately.

## File inventory

**Modified:**

- [`scripts/precommit-gauntlet.sh`](../../../scripts/precommit-gauntlet.sh) — add a
  `run_check "shell-check" -- make shell-check` step (full repo: syntax +
  ShellCheck + shfmt diff + `check-shell-policy.sh`). Place it in the lint phase
  near the existing `actionlint` / `lint-deploy` checks. The shfmt **diff** check
  is now active (safe post-W2).
- [`.github/workflows/ci.yml`](../../../.github/workflows/ci.yml) — extend the
  `config-lint` job (or add a dedicated `shell-quality` job) to: provision the
  pinned tools via `scripts/install-shell-tools.sh`, run `make shell-check` and
  `make shell-test`. Use the pinned-binary install pattern already used for
  gitleaks/hadolint in that job (`curl --fail --retry … && install -m 0755`),
  but driven through the provisioner so versions match local.
- actionlint config — point actionlint at the pinned ShellCheck binary (env
  `SHELLCHECK_OPTS`/explicit path, per actionlint docs) so workflow `run:`
  blocks are linted by `0.11.0`, not whatever is on the runner `PATH`.
- The agent changed-file path — add `scripts/shell-quality.sh changed <base>` to
  the write/commit hook flow so edits to a `.sh` get fast local validation
  without running the full-repo scan. Must stay **zero-network** and under the
  latency budget.

**New:**

- `scripts/tests/shell-enforcement.test.sh` — asserts the gauntlet step exists
  and that `changed <base>` returns non-zero on a planted lint/format/policy
  violation but zero on a clean diff. **Fails before** the wiring exists.

## Latency budget (from the master plan, must be proven)

- Changed-file validation **< 1.5 s** at current repo size.
- Full shell validation **< 5 s** (excluding one-time provisioning).
- Measured full-file costs: ShellCheck ~1.8 s, shfmt ~0.01 s, `bash -n` below
  timer resolution. The changed-file path must scope to diffed files to hit
  1.5 s — verify empirically and record the number in the commit message.

## Steps (TDD order)

1. Write `scripts/tests/shell-enforcement.test.sh` (failing) + `chmod +x`.
2. Add the `shell-check` `run_check` to the gauntlet.
3. Add the CI step/job (provisioner + `make shell-check` + `make shell-test`).
4. Point actionlint at the pinned ShellCheck.
5. Wire `shell-quality.sh changed <base>` into the agent write/commit path.
6. Measure both latencies; confirm budgets; record numbers.
7. `/precommit` (now self-enforcing) → commit → `/refactor` → push. Watch the CI
   `config-lint`/`shell-quality` job go green.

## Determinism requirement

The validation runner performs **no network access** and must produce identical
results locally and in CI. Provisioning (which does fetch) is a separate,
idempotent step; the runner fails with a prerequisite error if the exact tools
are absent — it never silently skips (mirrors the no-silent-skip rule).

## Out of scope

- Composite-action extraction (**W5**) and behavioral tests (**W6**).

## Reviewer checklist

- [ ] Gauntlet runs `make shell-check`; a planted lint/format/policy violation
      blocks the commit (no marker bypass — it re-runs every attempt).
- [ ] CI provisions pinned tools through `install-shell-tools.sh` and runs
      `make shell-check` + `make shell-test`; versions match local exactly.
- [ ] actionlint invokes the **pinned** ShellCheck, not runner `PATH`.
- [ ] `changed <base>` is diff-scoped, zero-network, and measured **< 1.5 s**;
      full scan **< 5 s**. Numbers recorded.
- [ ] Runner fails loudly on missing tools — no successful skip path.
- [ ] W4 lands strictly after W2 + W3 (no pre-baseline shfmt diff, no
      unclassified-script failures at first enforcement).
