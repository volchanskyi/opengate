# Retire the doc-link baseline: scope out ephemeral plans, fix real debt, delete the file

## Context

`.claude/doc-link-baseline.txt` is a 526-line ledger of SHA256 fingerprints, one per
known-broken repository-local Markdown link, that the gauntlet's `doc links` check
(`scripts/precommit-gauntlet.sh:234`) uses to suppress pre-existing debt while blocking
new violations. It "keeps growing" and churns on every docs commit.

**Root cause (measured):** of 561 current problems, **494 (94%) live in
`.claude/plans/archive/**`**. Archiving a plan freezes it, but its internal relative
links (to sibling plans, to code that has since moved/been deleted) rot — and each rotted
link becomes new baselined debt. So the baseline grows by ~N lines on every plan archive.
The remaining **67** problems are genuine, fixable rot in durable docs: **54 in
`.claude/phases.md`** (links to moved/removed code paths) and **13** across `docs/` and a
few ADRs.

Archived plans are ephemeral and deletion-bound by explicit doctrine
(`.claude/rules/plans-and-adrs.md`, ADR-036 §4) — link-checking a frozen graveyard slated
for deletion is pure noise. A "single signature" would shrink the diff but not stop the
churn (still regenerated on every archive). The better fix removes the noise at the root
and drives the durable-doc debt to zero, so the file can be **deleted entirely** — matching
ADR-036 §3's stated success signal ("the doc link-checker reports zero broken links").

**Outcome:** the checker stops scanning `.claude/plans/**`; the 67 real broken links are
fixed; the baseline file, its `--baseline`/`--write-baseline` machinery, and the gauntlet
flag are removed. The gate becomes a clean "zero broken links" with no ledger to maintain.

## Approach

### 1. Scope the checker out of the plan working-area (root-cause fix)

In `scripts/check-doc-links/scan.go`, add a single scope predicate that excludes any path
under `.claude/plans/` (covers both active plans and `archive/`), and route **both** the
directory walk (`collectMarkdownPath`) and the overlay scoping (`addOverlayFiles` /
`isScopedMarkdown`) through it, so the `--hook` path and the full scan agree.

- Keep the existing `docs/**` + `.claude/**` roots; just subtract `.claude/plans/**`.
- Reference: current scoping lives in `collectMarkdownPath` (scan.go:91) and
  `isScopedMarkdown` (scan.go:128). Centralize into one `inScope(relPath) bool`.

This alone drops 494/561 problems and permanently stops the per-archive growth. Note: the
plan-link *policy* (`planLinkIssue`, checker.go:138) is unaffected for durable sources —
`docs/**` and `.claude/*.md` are still scanned, so a docs→plan or ADR→active-plan link is
still caught; we only stop treating plan files *themselves* as link sources.

### 2. Fix the 67 residual broken links

For each `target does not exist` / anchor problem, repoint to the target's current location
or, if the target is genuinely gone, convert the link to plain text. Files:

- `.claude/phases.md` (54 — dead code paths under `web/src/...`, `agent/...`, `scripts/...`, `.github/...`)
- `docs/Security-and-Dependencies.md` (3), `docs/CI-Pipeline.md` (3), `docs/Agent-Updates.md` (1)
- `docs/adr/ADR-021-*.md` (2), `docs/adr/ADR-020-*.md` (1), `docs/adr/ADR-017-*.md` (1)
- `.claude/skills/wiki-audit/SKILL.md` (2)

Enumerate live targets with `GO111MODULE=off go run ./scripts/check-doc-links` and resolve
each against the current tree (the doc-link hook only blocks *newly* introduced problems, so
fixing is never blocked).

### 3. Remove the baseline machinery

- Delete `.claude/doc-link-baseline.txt`.
- `scripts/precommit-gauntlet.sh:234` — drop `--baseline .claude/doc-link-baseline.txt`
  (invoke the checker bare).
- `scripts/check-doc-links/main.go` — delete the `--baseline` and `--write-baseline` flags
  and the now-dead functions: `writeRequestedBaseline`, `applyBaseline`, `readBaseline`,
  `parseBaselineEntry`, `writeBaseline`, `suppressBaseline`, `problemFingerprint`,
  `resolveAuxiliaryPath`, plus the `baselinePath`/`writeBaselinePath` option fields. Simplify
  `execute` to: collect problems → report. (`onlyNewProblems` stays — it's the `--hook`
  diff, unrelated to the baseline. Confirm no other caller of the removed helpers via
  `make dead-code`.)

### 4. Tests (TDD — write/adjust these FIRST)

- **New failing test** (scope exclusion): in a Go test (extend
  `scripts/check-doc-links/checker_test.go` or add `scan_test.go`), build a temp tree with a
  `.claude/plans/x.md` containing a broken link and assert `check()` returns **no** problem
  for it, while a broken link in `docs/x.md` is still reported. This is the branch's
  first-touched test that unlocks the TDD gate.
- `scripts/tests/check-doc-links.test.sh` — remove the `--write-baseline`/`--baseline`
  round-trip test (lines ~110-111) and update the gauntlet-wiring assertion (line ~163) to
  assert the gauntlet invokes the checker **without** `--baseline`.

### 5. Docs

- `.claude/rules/plans-and-adrs.md:41` — reword: the checker refuses active-plan links from
  durable sources (`docs/**`, `.claude/*.md`); plan files themselves are no longer scanned as
  sources (ephemeral, deletion-bound).
- `docs/adr/ADR-036-*.md` §3 — note that ephemeral plan files are out of the link-checker's
  scan scope, so the "zero broken links" signal is now literally enforced with no baseline.
- Do **not** edit the two archived plans that mention the old `--baseline` command
  (`audit-wiki-drift.md`, `pentest-gate-semgrep-precommit.md`) — they're out of scan scope
  and slated for deletion.

## Verification

- `GO111MODULE=off go run ./scripts/check-doc-links` → exit 0, **no output** (zero problems).
- `GO111MODULE=off go test ./scripts/check-doc-links` → new scope test passes; plan-policy
  tests still green.
- `make shell-test` → `check-doc-links.test.sh` passes with the updated assertions.
- `make dead-code` → no new unused-symbol findings from the trimmed `main.go`.
- `ls .claude/doc-link-baseline.txt` → gone; `grep -rn 'doc-link-baseline\|--baseline' scripts/`
  → no matches outside pentest's unrelated `--baseline-commit`.
- Full `/precommit` gauntlet green, then commit → `/refactor` → push (per workflow rules).
