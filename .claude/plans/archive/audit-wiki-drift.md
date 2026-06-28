# Audit Plan — Wiki / Docs Drift Sweep (docs/ + README)

**Skill:** `/wiki-audit` (run in diagnostic/plan mode — fixes documented, not applied).
**Branch:** `dev`. **Owner:** engineer (docs).
**Date:** 2026-06-27. **Status:** Ready for review.

## Scope & method

Greped `docs/**/*.md` + root `README.md` for the drift-prone patterns
(percentages, version pins, file paths, ports, env vars, broken links) and
verified each against its source of truth, per the link-over-paraphrase
convention in [`docs/README.md`](../../../docs/README.md). The combined historical
ADR log (001–012) is frozen and excluded.

## Confirmed clean (evidence)

- **Broken links:** `GO111MODULE=off go run ./scripts/check-doc-links --baseline
  .claude/doc-link-baseline.txt` → exit 0 (also validates the five new audit
  plans). Link integrity is additionally gated by the gauntlet step and the
  `docs-validate` workflow.
- **Coverage numbers are correct:** every "80%" claim matches the enforced
  thresholds — Go `THRESHOLD=80` ([`ci.yml:238`](../../../.github/workflows/ci.yml#L238)),
  Web `THRESHOLD=80` ([`ci.yml:410`](../../../.github/workflows/ci.yml#L410)), Rust
  `--fail-under-lines 80` ([`ci.yml:115`](../../../.github/workflows/ci.yml#L115)),
  Sonar new-code ≥80. The Go exclusion list in
  [`CI-Pipeline.md:120`](../../../docs/CI-Pipeline.md#L120)
  (`testutil`/`metrics`/`openapi_gen.go`) matches the ci.yml grep.
- **Migration drift handled:** SQLite is referenced only as the historical
  cutover source ([`Database.md:39,220`](../../../docs/Database.md#L39)); Caddy /
  Compose are explicitly labelled "dormant"/"former" and mapped to
  ingress-nginx ([`Infrastructure.md:367`](../../../docs/Infrastructure.md#L367),
  [`Kubernetes.md:49`](../../../docs/Kubernetes.md#L49),
  [`Security-and-Dependencies.md:155`](../../../docs/Security-and-Dependencies.md#L155)).

## Findings

| # | Sev | Finding | File:line | Source of truth |
|---|-----|---------|-----------|-----------------|
| 1 | MEDIUM | "Go version **1.23.6** is pinned in CI" is stale by three minors — actual is **1.26**. | [`README.md:50`](../../../README.md#L50) | [`go.mod:3`](../../../server/go.mod#L3) (`go 1.26.0`); ci.yml `go-version: '1.26'` (lines 145/203/263) |
| 2 | LOW | Coverage `80%` is hard-coded as a literal in several docs (currently correct, but rots silently if a threshold changes). Per link-over-paraphrase, link to the enforcing line instead. | [`README.md:45`](../../../README.md#L45), [`Testing.md:92`](../../../docs/Testing.md#L92), [`CI-Pipeline.md:116,120-122,166`](../../../docs/CI-Pipeline.md#L116) | `ci.yml` THRESHOLD lines 115/238/410 |
| 3 | LOW | Verify the Rust coverage **exclusion list** in prose (`main.rs`, `webrtc.rs`, `terminal.rs`, `session/mod.rs`, `session/relay.rs`, `tests/`) still matches the `cargo-llvm-cov` config — exclusion lists are classic drift. | [`CI-Pipeline.md:121`](../../../docs/CI-Pipeline.md#L121) | Rust llvm-cov config / `ci.yml` Rust coverage step |

## Remediation plan

### Phase A — fix the correctness drift (~15 min, docs-only)

1. **F1:** in `README.md:50`, replace "Go version 1.23.6 is pinned in CI" with
   the current pin **linked** to its source — link `server/go.mod` (the `go`
   directive) and/or the CI `go-version`, rather than re-paraphrasing a bare
   number. *(Done-when: no doc states a Go version that disagrees with go.mod.)*

### Phase B — de-paraphrase the thresholds (~30 min, docs-only)

2. **F2:** convert the standalone `80%` coverage literals to links to the
   `ci.yml` THRESHOLD lines (keep one number only where it sits adjacent to the
   link in a summary table, per `docs/README.md`).
3. **F3:** diff the documented Rust exclusion list against the live llvm-cov
   config; fix or link it. If they match, link it so future edits stay honest.

### Validation

- Re-run `GO111MODULE=off go run ./scripts/check-doc-links --baseline
  .claude/doc-link-baseline.txt` and the `docs-validate` mermaid parser.
- `grep -rniE 'go .*1\.2[0-5]' docs/ README.md` returns no stale Go pin.

## File inventory

**Modify:** `README.md`, `docs/Testing.md`, `docs/CI-Pipeline.md`
(+ `docs/README.md` only if a convention example needs updating).

## Acceptance criteria

1. No doc states a version/percentage that disagrees with its source of truth.
2. Coverage + version facts are links, not paraphrased literals (except adjacent
   summary-table numbers).
3. `check-doc-links` and `docs-validate` stay green.

## Reviewer checklist

- [ ] Go version fact links to go.mod / ci.yml, not a hard-coded number.
- [ ] `80%` literals replaced with links (or justified summary-table adjacency).
- [ ] Rust exclusion list verified against the live config.
- [ ] Historical ADR log (001–012) untouched; per-file ADRs (013+) edited in place if needed.
