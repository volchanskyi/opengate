# Micro-Plan D3: CI-Only Mermaid Syntax Validation

**Parent master:** `diagrams-as-code-part-2.md` (§5 D3). **Branch:** `dev`.
**Owner:** CI. **Depends on:** nothing (but pairs with D2 — must accept C4).

## 1. Goal

Catch malformed Mermaid before GitHub renders an error box, using a **no-Puppeteer**
validator in **CI only** — the local gauntlet stays grep-only/zero-network.

## 2. Scope

**In:** a CI job that validates every `mermaid` fence under `docs/**`; pinned tool;
**C4-aware**; fails CI on a syntax error.
**Out:** adding any Mermaid runtime to the local hook; rendering/screenshotting (that is
D2's GitHub render gate); adding the validator to `web/`'s app dependencies.

## 3. Tool selection (decide in-plan)

- **Preferred: `@zabaca/mermaid-validate`** — uses the **official Mermaid parser** (+
  jsdom), so it accepts experimental **C4** syntax that D2 introduces.
- Alt: `probelabs/maid` (pure-JS, faster) — **only if** its grammar covers C4; otherwise
  it will false-fail D2's C4 blocks.
- **Version-align** the validator's Mermaid to GitHub's renderer as closely as possible
  (record the version); the D2 GitHub render gate remains authoritative.

## 4. File inventory

| File | Change |
|---|---|
| `.github/workflows/docs-validate.yml` | **New** workflow: `on: pull_request`/`push` path-filtered to `docs/**`; `actions/setup-node@v6` + node 24 (mirror [`ci.yml`](../../.github/workflows/ci.yml) line ~403); extract ` ```mermaid ` blocks from `docs/**.md` and validate each; fail on error. |
| `scripts/validate-mermaid.mjs` (or `.sh` wrapper) | **New.** Extract Mermaid fences from a Markdown file set and run the validator; pin the validator version (exact, e.g. via a committed `package.json`+lockfile under a `tools/mermaid-validate/` dir, or a digest-pinned `npx`). |
| `tools/mermaid-validate/package.json` + lockfile | **New (if used).** Isolated, pinned validator dependency — **not** mixed into `web/`. |

## 5. Approach (TDD-ish)

1. Add a fixture: a doc with a **deliberately malformed** Mermaid block.
2. Wire the workflow + extractor; confirm CI **fails** on the malformed fixture and
   **passes** on the current valid docs (incl. D2's C4 blocks once they land).
3. Remove the malformed fixture; pin the validator version/digest.
4. Confirm the local gauntlet is **unchanged** (no Mermaid runtime added locally).
5. `make shell-quality` (for any Bash wrapper) + `/precommit` green.

## 6. Acceptance criteria / DoD

- [ ] A malformed Mermaid block **fails** the `docs-validate` CI job; valid blocks
      (including C4) **pass**.
- [ ] Validator version is **pinned**; its Mermaid version recorded for alignment with
      GitHub.
- [ ] Job is path-filtered to `docs/**`; **no** validator added to the local gauntlet or
      to `web/` app deps.
- [ ] `/precommit` green; local hook still grep-only.

## 7. NFRs

- **Performance:** CI-only; local hook untouched. Job runs only on `docs/**` changes.
- **Security:** the validator enters the CI trusted path — pin by version/lockfile,
  isolate from app deps, scope to `docs/**`. Prefer the maintained official-parser tool.
- **Maintainability:** one validator; C4-aware so D2 doesn't fight it.

## 8. Reviewer/QA checklist

- [ ] Malformed-block failure demonstrated in a CI run (link attached).
- [ ] Validator pinned + isolated (not in `web/package.json`).
- [ ] Confirmed it accepts D2's C4 blocks (no false failure).
- [ ] Local gauntlet diff shows **no** new Mermaid runtime.

## 9. Risks

- Validator Mermaid version may diverge from GitHub's → it could pass a block GitHub
  can't render (esp. experimental C4). Mitigation: D2's GitHub render gate is
  authoritative; D3 is a fast pre-filter, not the final word.
