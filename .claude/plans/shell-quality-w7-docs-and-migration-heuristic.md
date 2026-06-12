# W7 — Documentation, Migration Heuristic, and Archive

**Parent:** [`shell-quality-hardening.md`](shell-quality-hardening.md) · Commit 7
of 7.

## Goal

Make the new shell-quality system discoverable and durable: canonical commands
and strict-mode classes in `/docs`, the shared agent/tooling rules updated, a
recorded Bash-vs-Go migration heuristic so future Shell growth is governed, and
the master plan archived.

## File inventory

**Modified:**

- [`docs/`](../../docs/) — add a Shell Quality page (canonical commands
  `make shell-check` / `shell-fmt` / `shell-test` / `shell-quality`; the three
  strict-mode classes; where the pinned versions live). Follow
  [`docs/README.md`](../../docs/README.md): **link, don't paraphrase** — point at
  `.shellcheckrc`, the `.editorconfig` shfmt block, and
  `scripts/install-shell-tools.sh` for versions; do not copy version numbers into
  prose. Link the page from [`docs/Home.md`](../../docs/Home.md).
- [`.claude/rules/tooling.md`](../rules/tooling.md) — add the canonical
  `make shell-*` targets to the Commands list (mirrors how `make e2e` etc. are
  documented), and the changed-file runner for the agent path.
- [`.claude/techdebt.md`](../techdebt.md) — close any shell-quality debt rows
  this effort resolved; open a row for anything W6 flagged as needing a real
  dependency seam.
- [`AGENTS.md`](../../AGENTS.md) is a symlink to CLAUDE.md — no separate edit;
  the rule update flows through `tooling.md`.

**Possibly new:**

- A short "Bash vs Go" migration heuristic — as a `/docs` section (preferred) or
  a lightweight ADR if it constitutes a real architectural decision. **If an
  ADR:** new immutable file in [`docs/adr/`](../../docs/adr/) + an index row in
  [`.claude/decisions.md`](../decisions.md); the ADR must **not** link this plan
  file (write-guard `adr-plan-link`) — put any "see working plan" pointer in
  `decisions.md`, and the rationale inline in the ADR.

## Migration heuristic to record

Move a script to Go only when it owns **persistent state, concurrency, a complex
parser, or a multi-command API** that is hard to test with command stubs — not
on size alone, and never to reduce the Shell language percentage (Option D in the
master plan). Thin launchers and CI glue stay Bash. The downloaded installer
stays one Shell file.

## Steps

1. Touch/strengthen the covering test for any rule/doc-validation script if a
   `/docs` check script is edited (most of this commit is `.md`, but the
   `make shell-*` rule wording must match reality — run `make shell-check` after
   editing to confirm the documented commands exist and pass).
2. Write the `/docs` Shell Quality page; link from `Home.md`.
3. Update `tooling.md` and `techdebt.md`.
4. Decide doc-section vs ADR for the migration heuristic; if ADR, add file +
   `decisions.md` row (no plan link in the ADR).
5. Run the full acceptance-criteria checklist from the master plan; confirm all
   green.
6. Archive `shell-quality-hardening.md` and the seven micro-plans to
   [`.claude/plans/archive/`](archive/).
7. `/precommit` → commit → `/refactor` → push.

## Master-plan acceptance criteria (verify all before archiving)

- [ ] Pinned ShellCheck/shfmt used locally and in CI.
- [ ] `make shell-check` validates syntax, ShellCheck, shfmt, strict-mode policy,
      workflow shell, and composite-action extraction.
- [ ] `make shell-test` discovers and runs all shell tests, no network.
- [ ] The eleven original harnesses remain green.
- [ ] Every ShellCheck finding fixed or narrowly justified; shfmt reports no diff.
- [ ] Every script has an enforced execution class; sourced libs preserve caller
      options.
- [ ] Critical scripts have deterministic stub tests (or a flagged real-seam
      debt row).
- [ ] Composite actions contain no complex unlinted multiline shell.
- [ ] Commit + CI gates fail on missing tools, lint, format drift, policy
      violations, or test failures.
- [ ] Changed-file validation meets the latency target.
- [ ] No production behavior changed merely to reduce the Shell percentage.

## Reviewer checklist

- [ ] `/docs` page links sources for versions/config (no paraphrased numbers);
      linked from `Home.md`.
- [ ] `tooling.md` lists the `make shell-*` targets; documented commands
      actually exist and pass.
- [ ] Migration heuristic recorded; if an ADR, it is immutable, indexed in
      `decisions.md`, and contains **no** plan-file link.
- [ ] `techdebt.md` reflects what was closed and what W6 deferred.
- [ ] Master-plan acceptance criteria all checked; plan + micro-plans archived.
