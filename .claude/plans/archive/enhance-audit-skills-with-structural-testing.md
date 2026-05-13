# Enhance Audit/Test Skills with Structural Testing Concepts

## Context

The seven editable skills (frontend-audit, backend-audit, infra-audit, tests-audit, precommit, refactor, admin-infra-oci) describe **what to check** in OWASP/lint terms. They overlap on "XSS" and "test gaps" and lack a precise vocabulary for **how thoroughly** a check went. Structural testing — control flow, data flow, mutation, slice-based, coverage — gives each skill a measurable, distinct lens.

User's decisions during brainstorming:
- **Built-in skills**: skipped (no SKILL.md). Also skipping `/wiki-audit` and `/observe`.
- **New tooling**: mutation testing (cargo-mutants, gremlins-rs, stryker) and static taint linting (gosec, eslint-plugin-security, eslint-plugin-no-unsanitized). Defer line→branch coverage upgrade.
- **Rewrite depth**: targeted patches per skill.
- **Overlap**: minor overlap is fine where the actively-used skill should own a concept; `/refactor` owns dead-code and unreachable-branch detection (not built-in `/simplify`).
- **CI gating**: all CI additions blocking from day one. No `continue-on-error`.
- **Mutation scope**: full-tree gating — every line, no diff grandfathering.
- **Baseline findings**: fix every finding from gosec / ESLint security plugins / dead-code sweep, regardless of count.

## Deploy pipeline constraint

The full chain — `push dev → 19 CI jobs → auto-merge to main → build-image → cd → staging (auto) → production (manual approval)` — is fully gated. Any new required check that fails blocks all four stages (auto-merge, build-image, staging deploy, production deploy). The repo has zero `continue-on-error: true` precedents.

**This means hard-gating from day one is only safe if the codebase is already clean against the new gates when they land.** A single mega-PR that introduces gates *and* cleans the baseline is too large to review responsibly. The plan below stages the work so each PR lands green under the existing gates, and a final small PR flips the new gates on.

## Concept → skill ownership

| Structural concept | Owning skill | Tooling |
|---|---|---|
| Mutation score / test-suite quality | **tests-audit** | cargo-mutants, gremlins-rs, stryker |
| Coverage gate enforcement (existing 80% line) | **precommit** | unchanged today; cites mutation report |
| Data flow / taint to **server** sinks | **backend-audit** | gosec, CodeQL go-queries |
| Data flow / taint to **DOM** sinks | **frontend-audit** | eslint-plugin-security, eslint-plugin-no-unsanitized, CodeQL js-queries |
| Config flow / sensitive-value propagation | **infra-audit** | grep + manual trace; no new tool |
| Slice-based impact analysis + dead-code/unreachable-branch detection | **refactor** | clippy `dead_code`, staticcheck `U1000`, ts-prune |
| Post-deployment control-flow validation | **admin-infra-oci** | terraform plan/apply diff; manual checklist |

## Rollout sequence (each step is a separate PR landing green on dev)

### PR 1 — Tooling install, no CI change

- [Makefile](Makefile): new targets `mutate-rust`, `mutate-go`, `mutate-web`, `mutate`, `taint-go`, `taint-web`, `dead-code`. Developer-facing only; CI unchanged.
- [web/package.json](web/package.json): devDeps `@stryker-mutator/core`, `@stryker-mutator/typescript-checker`, `@stryker-mutator/vitest-runner`, `eslint-plugin-security`, `eslint-plugin-no-unsanitized`, `ts-prune`. Plugins **not yet** registered in eslint config.
- New `web/stryker.config.json` (vitest runner, mutate `src/**/*.{ts,tsx}` excluding tests/generated). Threshold deferred to PR 9.
- New `server/.gosec.toml` (defaults; placeholder for future tuning).
- Rust mutation: cargo-mutants installed by developer; no `Cargo.toml` change.
- Verification: `make mutate`, `make taint-go`, `make taint-web`, `make dead-code` all run locally and produce reports. Existing CI passes (no gates added).

### PR 2 — Skill SKILL.md patches

Land all seven skill patches together (they reference tools installed in PR 1). Skills don't enforce gates themselves; they cite tool output.

1. [.claude/skills/precommit/SKILL.md](.claude/skills/precommit/SKILL.md) — new step **"Mutation diff gate"** (local) between coverage and SonarCloud steps. Add `make taint-go && make taint-web` and `make dead-code` to local lint section. Note: CI gates land in PR 9; until then, local checks are the early-warning system.
2. [.claude/skills/tests-audit/SKILL.md](.claude/skills/tests-audit/SKILL.md) — new **Section 0.5: Mutation analysis**. Surviving mutants are first-class findings; coverage % becomes a sanity check.
3. [.claude/skills/backend-audit/SKILL.md](.claude/skills/backend-audit/SKILL.md) — new **Section "Taint paths"** requiring source→sink documentation. Sources: HTTP body, query params, headers, env, externally-written DB rows. Sinks: SQL, `os/exec`, `io.Copy` to fs, response body, log args. Cite gosec rule IDs (G101, G201, G204, G304, G601).
4. [.claude/skills/frontend-audit/SKILL.md](.claude/skills/frontend-audit/SKILL.md) — new **Section "DOM taint paths"**. Sources: server response, query params, postMessage, localStorage, sessionStorage. Sinks: `innerHTML`, `dangerouslySetInnerHTML`, `href`, `src`, `eval`, `document.write`, `setTimeout(string)`. Cite eslint-plugin-security and eslint-plugin-no-unsanitized rule IDs.
5. [.claude/skills/infra-audit/SKILL.md](.claude/skills/infra-audit/SKILL.md) — new **Section "Sensitive-value flow trace"**. For each flagged secret: definition site → tfvars → cloud-init template → systemd unit → process argv → log line.
6. [.claude/skills/refactor/SKILL.md](.claude/skills/refactor/SKILL.md) — two new sections: **"Slice before you cut"** (callers + test slice as verification gate for any ≥2-file change), **"Dead-code & unreachable-branch sweep"** (run `make dead-code` on touched packages, remove or justify each finding).
7. [~/.claude/skills/admin-infra-oci/SKILL.md](file:///home/ivan/.claude/skills/admin-infra-oci/SKILL.md) (user-scope) — new **Section "Post-deployment control-flow validation"**: enumerate decision branches taken during apply (capacity, region failover, AD), verify resulting state.

### PR 3 — Dead-code baseline cleanup

Run `make dead-code` on the full tree. Fix every finding:
- Rust: clippy with `-W dead_code -D warnings` on agent + workspace crates. Remove unused fns/types/imports or justify with `#[allow(dead_code)]` + comment.
- Go: `staticcheck -checks U1000 ./...` on server. Same treatment.
- TS: `ts-prune` on web. Same treatment.
- Verification: tools clean locally; no behavior change; existing tests still pass.
- Checkpoint: if findings exceed ~50 across the tree, pause and triage with user.

### PR 4 — gosec baseline cleanup (Go server)

Run `gosec ./...` on server. Fix every finding. Verification: gosec exits clean, server tests still pass, no behavior change.
- Checkpoint at the start: count findings; if >100 pause and triage rule scope (e.g. test-code exclusion patterns) before mass-fixing.

### PR 5 — ESLint security baseline cleanup (web)

Register `eslint-plugin-security` and `eslint-plugin-no-unsanitized` in [web/eslint.config.js](web/eslint.config.js) at `error` severity (recommended rule sets). Fix every finding `npm run lint` surfaces. Verification: `npm run lint` clean, web tests pass.
- Checkpoint: same as PR 4.

### PR 6 — Rust mutation-test gap closure (agent)

Run `cargo-mutants` full-tree on agent. For each surviving mutant, write a test that kills it. Target: ≥70% mutation score. Realistic carve-outs documented in `cargo-mutants.toml` for genuinely-unmutateable code (platform shims, FFI boundaries, log-only paths).
- Checkpoint at start: run cargo-mutants, count surviving mutants. If reaching 70% requires >40 hours of test-writing, pause and discuss whether to lower the threshold or carve out specific modules.

### PR 7 — Go mutation-test gap closure (server)

Same as PR 6 for `gremlins unleash` on server. Target ≥70% mutation score.

### PR 8 — Web mutation-test gap closure

Same for `stryker run` on web. Target ≥70% mutation score (`thresholds.break: 70` configured but not yet enforced in CI until PR 9).

### PR 9 — Mutation testing as observability (superseded design)

> **Superseded.** The original "enable hard gates" design was reconsidered before implementation. Adding the mutation matrix to `merge-to-main.needs[]` would have tripled CI wall-clock from 6-8 min to 25-30 min for a leading-indicator signal that doesn't belong on the critical path. The redesigned PR 9 treats mutation score as observability data with regression alerts — not as a build gate.
>
> The detailed redesign lives in [`pr9-mutation-testing-as-observability.md`](pr9-mutation-testing-as-observability.md). Summary:
>
> - New `.github/workflows/mutation.yml` runs nightly at 03:00 UTC on a matrix `[rust, go, web]`; not wired into `merge-to-main`.
> - Per-run canonical row appended to `docs/mutation-history.jsonl` (rolling 90-day window).
> - Same row pushed to Loki via the existing deploy SSH tunnel + monitoring docker network; visualised in Grafana via a new "Mutation Testing Trend" dashboard.
> - Telegram alert on regression: drop >2pp from previous successful run **OR** absolute score <70%. Workflow exits red on regression so the GitHub Actions history reflects the dip.
> - `go-lint` gosec wiring was already absorbed into PR 4; ESLint security plugins already in PR 5. PR 9 simplifies to just the new async workflow.

## Verification (cross-cutting)

1. **Per PR**: tool reports clean locally; existing test suite passes; deploy still reaches staging healthy.
2. **After PR 9**: full chain (dev → main → build-image → cd → staging → prod) completes within wall-clock + 30 min of pre-plan baseline.
3. **Skill execution sample**: invoke each updated skill on a real diff and confirm reports include the new structural sections (mutation analysis, taint paths, secret flow trace, def-use slice, dead-code sweep, post-deploy decision branches).
4. **Phases.md** entry per PR.

## Risk register

- **Mutation cleanup may be huge.** PR 6/7/8 each may surface hundreds of surviving mutants; the 70% target may take weeks of test-writing per language. Mitigated by per-PR checkpoint where I pause and surface the surviving-mutant count before committing to write all tests.
- **Rust agent mutation score may plateau below 70%** because of platform shims (Wayland/X11/macOS/Windows) that have no test harness. Mitigation: explicit `cargo-mutants.toml` carve-outs documented per module; target adjusted only with user approval.
- **CI wall-clock budget**: the new `mutation-testing` job adds ~20–30 min in parallel. It is not on the e2e critical path, so total wall-clock to staging should not regress. Verify in PR 9 dry-run.
- **Auto-merge stalls**: if the new gate is flaky on a real change, dev→main auto-merge stalls and deploys halt. Mitigation: `mutation-testing` job uses retry-on-flake (`gh run rerun-failed`) pattern already used by e2e; document in CI README.

## Out of scope (explicit non-goals)

- Branch/condition/path coverage upgrade — deferred per user.
- Built-in skills — no editable prompt.
- `/wiki-audit`, `/observe` — explicitly excluded.
- New ADR — these are tooling/process changes, not architecture. Record in `phases.md` only.
