# Shell Quality Hardening and Enforcement

**Status:** Completed. Decomposed into seven micro-plans (see
[Micro-Plan Index](#micro-plan-index)); each is independently implementable and
reviewable.

## Executive Judgment

The analyst identified the right tools but applied two generic assumptions that
do not fit this repository:

1. `set -euo pipefail` is a useful default for standalone scripts, not a safe
   blanket rule for sourced libraries, failure-aggregating test harnesses, or
   the all-checks precommit runner.
2. Adding a shell test framework does not automatically improve test quality.
   OpenGate already has a substantial plain-Bash test suite; replacing it
   wholesale with Bats would create migration churn without closing the most
   important behavioral gaps.

The recommended option is a balanced hardening program:

- pin and enforce ShellCheck and shfmt;
- classify scripts by execution semantics before enforcing strict mode;
- preserve the existing plain-Bash tests;
- add behavioral tests to privileged, deployment, installer, and external-I/O
  scripts that currently lack them;
- extract complex composite-action shell into normal `.sh` files so the same
  gates cover it;
- retain Bash for orchestration and migrate only logic that has outgrown it.

The goal is not to reduce the Shell percentage. The goal is to make every Shell
entry point deterministic, reviewable, tested according to risk, and subject to
the same local and CI gate.

## Empirical Baseline

### Repository Surface

The current repository contains:

| Surface | Current size | Existing enforcement |
|---|---:|---|
| Tracked `.sh` files | 54 files / 7,342 physical lines | `bash -n` succeeds; no repository-wide ShellCheck or shfmt gate |
| GitHub workflow `run:` blocks | 193 blocks / 1,377 physical lines | [`actionlint`](../../../Makefile) checks workflows and invokes whichever ShellCheck is on `PATH` |
| Composite-action `run:` blocks | 14 blocks / 285 physical lines | Not covered by actionlint's workflow parser |
| Total shell-executed surface | About 9,004 physical lines | Split across inconsistent enforcement paths |

GitHub Linguist reports Shell as approximately 11.7% of language bytes. That
validates the analyst's estimate for tracked Shell files, but it excludes shell
embedded in workflow and composite-action YAML.

The largest tracked Shell files include:

- [`hooks.test.sh`](../../../scripts/tests/hooks.test.sh)
- [`bastion-session.sh`](../../../deploy/scripts/bastion-session.sh)
- [`precommit-gauntlet.sh`](../../../scripts/precommit-gauntlet.sh)
- [`build-image-gate.test.sh`](../../../scripts/tests/build-image-gate.test.sh)
- [`pentest-review.test.sh`](../../../scripts/tests/pentest-review.test.sh)
- [`common.sh`](../../../deploy/scripts/common.sh)
- [`mutation-summarize.sh`](../../../scripts/mutation-summarize.sh)
- [`install.sh`](../../../server/internal/api/install.sh)

The Shell share is therefore architectural, not accidental: hooks, CI gates,
deployment orchestration, release/install flows, and observability pipelines
all depend on it.

### Dialect and Compatibility

Every tracked script declares Bash. Bash-specific arrays, `[[ ... ]]`,
`BASH_SOURCE`, process substitution, and `/dev/tcp` are already used, so a POSIX
`sh` portability mandate would be a rewrite with no current benefit.

The downloaded [`install.sh`](../../../server/internal/api/install.sh) has a wider
runtime surface than repository-only scripts because it executes on managed
machines. Its minimum supported Bash and Linux distribution must be explicit
before adopting newer Bash features.

### Strict-Mode Distribution

The current first-option declarations are:

| Declaration | Files | Interpretation |
|---|---:|---|
| `set -euo pipefail` | 36 | Normal fail-fast executables |
| `set -Eeuo pipefail` | 1 | Fail-fast executable with inherited `ERR` trap |
| `set -uo pipefail` | 14 | Mostly aggregators, status inspectors, and tests |
| `set -e` | 1 | Incomplete strict-mode policy |
| No option declaration | 2 | Sourced function libraries |

Several `set -uo pipefail` scripts intentionally collect multiple failures or
inspect non-zero statuses. For example,
[`precommit-gauntlet.sh`](../../../scripts/precommit-gauntlet.sh) must continue
after a failed check so it can report the complete failure set.

The Bash manual documents that `errexit` has contextual exceptions in
conditions, `&&`/`||` lists, pipelines, functions, and subshells. A file can
therefore contain `set -e` and still require explicit status handling. See the
official [`set` builtin documentation](https://www.gnu.org/software/bash/manual/html_node/The-Set-Builtin.html).

Sourced libraries should not change the caller's shell options.
[`scripts/lib/loki-push.sh`](../../../scripts/lib/loki-push.sh) and
[`scripts/lib/postgres-prereq.sh`](../../../scripts/lib/postgres-prereq.sh) already
follow that rule. [`hooks/lib/common.sh`](../../hooks/lib/common.sh) and
[`deploy/scripts/common.sh`](../../../deploy/scripts/common.sh) currently set
global options despite being sourced; their callers already establish their
own policies, so the library declarations should be removed after tests pin the
behavior.

### Static-Analysis Baseline

ShellCheck `0.11.0` reports:

- zero warning-or-higher findings across all tracked `.sh` files;
- nine informational findings after resolving sources relative to each script;
- the remaining findings are indirect test functions, intentional nested-shell
  literals, and one unreachable helper tail.

This is a strong baseline. The defect is not widespread lint debt; it is the
absence of deterministic version pinning and enforcement. The workstation's
default ShellCheck is older than the current pinned candidate, proving that
using an arbitrary `PATH` version would make local and CI results diverge.

ShellCheck explicitly supports build integration and recommends pinning a
specific version to prevent surprise failures when new checks are released.
See the official [ShellCheck documentation](https://github.com/koalaman/shellcheck)
and [release](https://github.com/koalaman/shellcheck/releases/tag/v0.11.0).

### Formatting Baseline

A shfmt `3.13.1` dry run with the Google-like Bash style
`-i 2 -ci -bn` changes about 1,370 lines across 42 files. This is mostly
mechanical layout, not evidence of poor behavior.

The baseline must therefore land as an isolated formatting commit after any
in-progress work that edits hooks or scripts. Functional fixes and formatting
must not share a commit.

The shfmt `-kp` option reduces churn but is deprecated and scheduled for removal
in the next major version, so it should not become project policy. See the
official [shfmt documentation](https://github.com/mvdan/sh) and
[release](https://github.com/mvdan/sh/releases/tag/v3.13.1).

### Test Baseline

[`scripts/tests/`](../../../scripts/tests/) contains eleven plain-Bash test
harnesses. The precommit gauntlet discovers them by glob, so new
`*.test.sh` files automatically enter the commit gate.

Current tests are concentrated around:

- Claude hooks;
- TDD and architecture classifiers;
- image/release gates;
- Semgrep and PMAT gates;
- coverage and Postgres prerequisites;
- summary parsers.

The main direct behavioral gaps are in:

- [`install.sh`](../../../server/internal/api/install.sh);
- [`bastion-session.sh`](../../../deploy/scripts/bastion-session.sh);
- deployment, rollback, smoke, and wait scripts under
  [`deploy/scripts/`](../../../deploy/scripts/);
- Loki transport and query helpers;
- mutation and Terraform drift summarizers;
- the larger composite-action shell blocks under
  [`.github/actions/`](../../../.github/actions/).

Bats is a capable framework, but its files are evaluated once for discovery and
again in separate processes for each test. Its native syntax also needs special
handling from ShellCheck, shfmt, and editors. See the official
[Bats documentation](https://bats-core.readthedocs.io/en/stable/writing-tests.html)
and [release](https://github.com/bats-core/bats-core/releases/tag/v1.13.0).

## Scope

### Included

- All tracked Bash files.
- Shell embedded in GitHub workflow `run:` blocks.
- Shell embedded in local composite actions.
- Strict-mode and sourced-library policy.
- Static analysis, formatting, syntax validation, and deterministic tool
  provisioning.
- Risk-based behavioral tests for Shell-owned behavior.
- Shared local, precommit, and CI enforcement.
- Editor integration that invokes the canonical repository commands.
- A selective migration rule for scripts that become application programs
  rather than orchestration.

### Excluded

- Rewriting working scripts solely to lower the language percentage.
- POSIX `sh`, Zsh, Fish, or Windows shell compatibility.
- Mandating Bats or shUnit2 for every test.
- Shell line-coverage targets. Exit-path and side-effect assertions are more
  useful for orchestration scripts than a raw line percentage.
- Dockerfile `RUN` instruction linting, which remains owned by Hadolint.
- Formatting workflow YAML shell fragments with shfmt; GitHub expressions are
  not standalone Bash syntax.
- Functional changes hidden inside the formatting baseline.

## Architectural Constraints

- All existing scripts use Bash and should be analyzed as Bash.
- The precommit gauntlet deliberately aggregates failures and cannot use
  blanket `errexit`.
- Sourced libraries must preserve caller option state.
- Hook-time checks must remain deterministic, zero-network, and fast.
- Tool versions must be identical locally and in CI.
- Missing tools must fail loudly; no successful "skip" path is permitted.
- Provisioning must be idempotent and must not require a separate manual setup
  procedure.
- Release assets must be version-pinned and checksum-verified.
- The installer must remain deliverable as one Shell file.
- Composite actions receive inputs through GitHub expression expansion; any
  extracted scripts must receive explicit environment variables or arguments,
  not templated source code.
- The current-state documentation work touches hooks and should land before the
  shfmt baseline. The shell gates should land before implementing the CI/CD
  orchestration described by `context-driven-fault-injection.md`.

## Quality Metrics

| Concern | Required result |
|---|---|
| Syntax | Every tracked script parses as Bash |
| Static analysis | Zero unsuppressed ShellCheck findings at `style` severity with the pinned version |
| Formatting | Zero shfmt diff using the committed configuration |
| Workflow shell | actionlint passes while invoking the pinned ShellCheck |
| Composite actions | Multiline logic is extracted into gated `.sh` files; remaining inline commands are trivial |
| Strict mode | Every script is classified; standalone scripts use the default policy or a justified exception |
| Libraries | Sourced libraries do not mutate caller shell options |
| Tests | Every critical script has success, invalid-input, external-failure, and cleanup/idempotency coverage where applicable |
| Security | No unverified tool downloads, secret-bearing traces, unsafe temporary files, or unreviewed dynamic command construction |
| Local latency | Changed-file validation completes under 1.5 seconds at the repository's current size |
| Full latency | Full Shell validation completes under 5 seconds excluding one-time tool provisioning |
| Determinism | Validation performs no network access and produces the same result locally and in CI |
| Maintainability | One command owns syntax, lint, format, policy, and Shell tests |

The measured full-file costs are already compatible with these targets:
ShellCheck is about 1.8 seconds, shfmt about 0.01 seconds, and Bash parsing is
below timer resolution on the current workstation.

## Analyst Recommendations: Project-Specific Verdict

| Recommendation | Verdict | Reason |
|---|---|---|
| Start every script with `set -euo pipefail` | **Modify** | Correct default for standalone scripts; wrong for libraries and intentional failure aggregators |
| Add ShellCheck to IDE and CI | **Adopt** | Current code is clean, but the version and enforcement path are inconsistent |
| Enforce shfmt | **Adopt carefully** | Valuable after one isolated baseline; formatter flags must be pinned and non-deprecated |
| Add shUnit2 or Bats | **Do not mandate** | Existing plain-Bash tests are mature; framework migration does not close current risk gaps |
| Run lint/format on each pull request | **Adopt** | Use one shared runner from CI and the no-bypass commit gauntlet |

## Options

### Option A — Static Gates Only

Add pinned ShellCheck, shfmt, Bash parsing, Make targets, precommit integration,
and a dedicated CI job. Keep strict-mode declarations and tests unchanged.

**Benefits**

- Lowest implementation risk.
- Immediately prevents new quoting, formatting, and syntax debt.
- Fast enough for local and CI enforcement.

**Costs and gaps**

- Does not resolve library option leakage.
- Does not test untested privileged and deployment behavior.
- Leaves complex composite-action blocks outside normal `.sh` enforcement.
- Treats the current strict-mode distribution as policy by accident.

**Use when**

The project needs a small immediate guardrail while a fuller phase is deferred.

### Option B — Classified Policy and Risk-Based Tests

Implement Option A plus:

- executable/library/aggregator classification;
- machine-enforced strict-mode rules;
- removal of option mutation from sourced libraries;
- extraction of complex composite-action shell;
- targeted behavioral tests for critical operational scripts;
- an explicit migration heuristic for future Shell growth.

**Benefits**

- Addresses root causes rather than only style.
- Preserves the working test architecture.
- Covers the highest security and outage risks.
- Creates one policy for local agents, humans, commit hooks, and CI.
- Supports future fault-injection and deployment work without adding another
  ungoverned script layer.

**Costs and gaps**

- Requires several reviewable commits rather than one tooling commit.
- Critical-script testing needs command stubs and small dependency-injection
  seams.
- Composite-action extraction changes execution boundaries and needs workflow
  validation.

**Decision**

**Recommended.** It has the best quality-to-churn ratio.

### Option C — Full Bats Migration

Adopt Option B but convert all existing plain-Bash tests to Bats and require
Bats for new tests.

**Benefits**

- Standard test discovery, setup/teardown, filtering, and TAP/JUnit output.
- Convenient `run` assertions for command status and output.

**Costs and gaps**

- Rewrites eleven passing harnesses without changing production behavior.
- Adds another pinned runtime dependency.
- Bats native syntax requires special ShellCheck/shfmt handling.
- Process-per-test execution can make large suites slower.
- Migration effort competes with tests for currently uncovered scripts.

**Decision**

**Reject as a repository-wide migration.** Permit pinned Bats for a new test
suite only when its setup/teardown, tagging, or parallelism materially reduces
complexity. Do not introduce shUnit2.

### Option D — Reduce Shell by Rewriting in Go

Move stateful parsers, long-running controllers, or complex CLIs into Go while
retaining thin Shell launchers.

**Benefits**

- Strong typing, structured parsing, native unit tests, race detection, and
  easier composition for genuinely application-like tools.
- Can reduce risk in scripts that grow into state machines.

**Costs and gaps**

- Rewrites are high-risk and can regress mature operational behavior.
- Go binaries add build, release, cross-platform, and bootstrap concerns.
- The downloaded installer and much CI glue must remain Shell.
- Language percentage is not a quality metric.

**Decision**

**Use selectively, not as the main program.** Consider migration when a script
owns persistent state, concurrency, a complex parser, or a multi-command API
that is difficult to test with command stubs. Size alone is not sufficient.

## Decision Matrix

| Criterion | A: Gates | B: Balanced | C: Bats migration | D: Go migration |
|---|---:|---:|---:|---:|
| Immediate defect prevention | High | High | High | Medium |
| Behavioral risk reduction | Low | High | High | High after rewrite |
| Migration churn | Low | Medium | High | Very high |
| New dependencies | Two tools | Two tools | Three tools | Build/release tooling |
| Fits current architecture | Medium | High | Medium | Selective |
| Long-term maintainability | Medium | High | Medium | High only for suitable candidates |
| Recommendation | Interim | **Adopt** | Reject wholesale | Use case-by-case |

## Recommended Design

### Canonical Commands

Add:

- `make shell-check` — syntax, ShellCheck, shfmt diff, and policy.
- `make shell-test` — all `scripts/tests/*.test.sh` plus any deploy Shell tests.
- `make shell-fmt` — write formatting with the pinned shfmt.
- `make shell-quality` — `shell-check` followed by `shell-test`.

Back these targets with one repository runner, such as
`scripts/shell-quality.sh`, supporting:

- `check` for the full repository;
- `changed <base>` for fast local/hook validation;
- `format` for explicit writes;
- `test` for behavioral tests.

The runner must enumerate files from Git rather than broad filesystem traversal
so generated, ignored, vendored, and temporary scripts cannot alter results.

### Tool Pinning and Provisioning

Add an idempotent provisioner for:

- ShellCheck `0.11.0`;
- shfmt `3.13.1`.

The provisioner must:

- select a supported OS/architecture asset;
- verify a committed SHA-256 digest before installation;
- install under the OpenGate tool cache rather than system directories;
- no-op when the exact version is present;
- fail loudly on unsupported platforms;
- never use `latest`, an unpinned package-manager version, or `curl | sh`.

CI and session bootstrap call the provisioner automatically. The validation
runner itself performs no network access and exits with a prerequisite error
when the exact tools are unavailable.

### ShellCheck Policy

Commit a repository configuration that:

- analyzes Bash;
- resolves sourced files relative to the script directory;
- enables findings through `style`;
- excludes no rule globally without a documented repository-wide reason.

Resolve the current informational baseline in the first tooling commit:

- fix genuinely unreachable code;
- add narrow, justified directives for functions invoked indirectly by tests;
- retain narrow directives for strings intentionally expanded by an inner
  shell.

Suppressions must sit next to the relevant command and explain why the flagged
behavior is intentional.

### shfmt Policy

Commit EditorConfig-backed settings equivalent to:

```text
indent_style = space
indent_size = 2
binary_next_line = true
switch_case_indent = true
shell_variant = bash
```

Do not enable deprecated `keep_padding`. Apply the baseline to tracked `.sh`
files only.

### Strict-Mode Policy

Classify every tracked script as one of:

1. **Standalone fail-fast executable**
   - default: `set -euo pipefail`;
   - use `-E` only when an inherited `ERR` trap exists;
   - explicitly handle expected non-zero statuses.
2. **Failure-aggregating executable or test**
   - use `set -uo pipefail`;
   - capture and assert every expected status;
   - list the file and reason in the policy exception manifest.
3. **Sourced library**
   - do not call `set`, `shopt`, or install traps at file scope;
   - document exported functions;
   - return statuses rather than exiting unless the function contract says
     otherwise.

Add `scripts/check-shell-policy.sh` and a corresponding
`scripts/tests/check-shell-policy.test.sh`. The checker must reject:

- missing or non-Bash shebangs;
- unclassified exceptions;
- stale exception entries;
- libraries that mutate caller options;
- standalone scripts without the required strict declaration;
- unapproved `set +e` or broad option disabling.

### Embedded Shell

Keep workflow `run:` blocks under actionlint and explicitly point actionlint to
the pinned ShellCheck binary.

For [`.github/actions/`](../../../.github/actions/):

- extract multiline logic into adjacent `.sh` files;
- pass action inputs through explicit environment variables or arguments;
- keep `action.yml` steps as short invocations;
- add command-stub tests for the extracted scripts;
- validate the action metadata with the existing configuration gate.

Prioritize
[`verify-oci-tfstate-creds/action.yml`](../../../.github/actions/verify-oci-tfstate-creds/action.yml),
which currently contains the largest inline Shell blocks.

### Test Strategy

Retain the current plain-Bash convention. Add a small shared assertion/stub
library only when the first new tests demonstrate repeated code; do not refactor
all existing tests pre-emptively.

Classify production scripts by risk:

| Tier | Examples | Required testing |
|---|---|---|
| Critical | installer, deployment/rollback, credentials, firewall/OCI setup | Success, malformed input, dependency failure, cleanup, idempotency, secret-redaction |
| Gate/parser | build/release gates, summarizers, classifiers | Decision table and malformed fixture coverage |
| Thin wrapper | one-command launchers with no branching | Static gates plus one smoke test where practical |

First behavioral targets:

1. [`install.sh`](../../../server/internal/api/install.sh) with stubbed `curl`,
   `systemctl`, `install`, and checksum commands.
2. [`bastion-session.sh`](../../../deploy/scripts/bastion-session.sh) with stubbed
   OCI and SSH commands, cache fixtures, and cleanup assertions.
3. [`deploy/scripts/`](../../../deploy/scripts/) with fake Docker/Compose state.
4. Loki push/query transports with stubbed `ssh`, `kubectl`, and `curl`.
5. Extracted composite-action scripts.

Tests must not require live OCI, Kubernetes, Docker, systemd, or network access.

### IDE Integration

IDE diagnostics are advisory; the repository runner is authoritative.

Add VS Code tasks for `make shell-check` and `make shell-fmt`. Extension
recommendations may be added, but they must use the repository configuration
and must not introduce a second formatter policy or an unpinned CI dependency.

## Ordered Implementation

### Commit 1 — Policy Tests and Tool Provisioning

- Add failing tests for tool-version, file enumeration, and policy behavior.
- Add the pinned, checksum-verifying provisioner.
- Add ShellCheck and shfmt configuration.
- Add `scripts/shell-quality.sh` and Make targets.
- Resolve the small ShellCheck informational baseline.
- Keep shfmt in check-only mode until the baseline commit.

### Commit 2 — Isolated shfmt Baseline

- Run the pinned formatter over every tracked `.sh` file.
- Make no functional or documentation changes.
- Run the complete Shell test suite before and after formatting.
- Review high-risk scripts separately to confirm heredocs, traps, and nested
  shell strings are unchanged semantically.

### Commit 3 — Strict-Mode Classification

- Add the explicit exception manifest and policy checker.
- Remove file-scope option mutation from sourced libraries.
- Upgrade the incomplete `set -e` script or document its justified class.
- Keep aggregators on explicit status handling rather than forcing `errexit`.
- Add regression tests before each behavior-affecting edit.

### Commit 4 — Universal Enforcement

- Add `make shell-check` to
  [`precommit-gauntlet.sh`](../../../scripts/precommit-gauntlet.sh).
- Add a dedicated fast Shell quality job to
  [`ci.yml`](../../../.github/workflows/ci.yml).
- Configure actionlint to invoke the pinned ShellCheck.
- Add changed-file enforcement to the agent write/commit path while retaining
  full-repository CI enforcement.
- Prove the changed-file path stays under the latency budget.

### Commit 5 — Composite-Action Extraction

- Extract the largest multiline action blocks into adjacent `.sh` files.
- Add offline command-stub tests.
- Keep GitHub-expression interpolation in YAML and pass only validated values
  to scripts.
- Validate every affected workflow and composite action.

### Commit 6 — Critical Behavioral Tests

- Test the installer without root, network, or systemd.
- Test bastion cache/session/error behavior.
- Test deploy, rollback, smoke, and wait failure paths.
- Test Loki transport selection and cleanup.
- Refactor only the seams required for deterministic tests.

### Commit 7 — Documentation and Migration Heuristic

- Document canonical commands and strict-mode classes under [`docs/`](../../../docs/).
- Update the shared agent/tooling rules with the canonical commands.
- Record when new logic belongs in Bash and when it should move to Go.
- Archive this plan after every acceptance criterion is met.

## Acceptance Criteria

- Pinned ShellCheck and shfmt versions are used locally and in CI.
- `make shell-check` validates syntax, static analysis, formatting, strict-mode
  policy, workflow shell, and composite-action script extraction.
- `make shell-test` discovers and runs all Shell tests without network access.
- The existing eleven Shell test harnesses remain green.
- Every ShellCheck finding is fixed or narrowly justified.
- shfmt reports no diff.
- Every script has an enforced execution class.
- Sourced libraries preserve caller shell options.
- Critical scripts have deterministic command-stub tests.
- Composite actions contain no complex unlinted multiline shell.
- Commit and CI gates fail on missing tools, lint findings, formatting drift,
  policy violations, or test failures.
- Changed-file validation meets the hook latency target.
- No production behavior changes merely to reduce the Shell percentage.

## Locked Decisions

Settled with the maintainer on 2026-06-11. These are inputs to the micro-plans;
do not re-litigate them inside a micro-plan.

1. **Scope:** Full Option B as written — static gates **plus** strict-mode
   classification with a machine-enforced policy checker (`check-shell-policy.sh`
   + exception manifest), composite-action extraction, and the risk-based
   behavioral-test tier. Options A/C/D are not taken.
2. **shfmt baseline timing:** land **now, ahead of all other in-progress work**
   (the six `fast-path-*` plans and `context-driven-fault-injection.md`), so
   subsequent work enters already-formatted and under the gate. See the two
   guardrails below for the mechanics this forces.
3. **Policy checker:** **build it** — the bespoke classifier and exception
   manifest are in scope (Commit 3 / W3), not deferred to ShellCheck alone.
4. **Bats:** remains opt-in only; the eleven plain-Bash harnesses are not
   migrated (Option C stays rejected).
5. **Installer compatibility floor:** Bash 4.4-compatible syntax for
   installer/deploy scripts until evidence supports a newer floor.

### Guardrail A — "shfmt now" mechanics

The maintainer chose to land the formatting baseline ahead of the in-progress
branches. Two repo-specific hazards must be handled, neither of which the
external analysts noted:

- **The TDD / source-write hooks gate the reformat.** A mass `.sh` reformat is a
  source edit, so [`pretooluse-bash-source-write-guard.sh`](../../hooks/pretooluse-bash-source-write-guard.sh)
  and [`pretooluse-tdd-gate.sh`](../../hooks/pretooluse-tdd-gate.sh) require a test
  change to already exist on the branch. W1 lands its tests first; because the
  TDD gate goes silent for the rest of the branch once any test change exists,
  W2–W7 inherit a satisfied gate. The W2 baseline must therefore land **after**
  W1 on the same `dev` line.
- **shfmt is not wired into the commit gauntlet until W4.** W1 adds `make
  shell-check` with ShellCheck enforcement only; the shfmt **diff** check is not
  added to [`precommit-gauntlet.sh`](../../../scripts/precommit-gauntlet.sh) until W4,
  which lands *after* the W2 baseline. This prevents the ~1,370-line pre-baseline
  diff from blocking W1's own commit.
- **Active-branch rebase:** the W2 baseline lands as a single atomic
  formatting-only commit on `dev` so the six fast-path branches each rebase over
  one stable, formatting-only target.

### Guardrail B — behavioral tests must not become false greens

Full Option B includes W6's command-stub tests for `install.sh`,
`bastion-session.sh`, and the deploy scripts. These are held to the same bar as
the [test-determinism rule](../../rules/tests-determinism.md): a stub must let the
script exercise **real** branching, cleanup, and idempotency logic — assert
observable side-effects (files created/removed, exit codes, redaction) and exit
paths, never merely "the stub was invoked." Any script where a meaningful
offline test is impossible without live infra is flagged in the micro-plan for a
real dependency seam, **not** given a fake-green stub test.

## Micro-Plan Index

Seven self-contained micro-plans, one per ordered commit. Each carries its own
file inventory, steps, and reviewer checklist. They land in order on `dev`.

| # | Micro-plan | Commit theme |
|---|---|---|
| W1 | [`shell-quality-w1-tooling-and-policy-tests.md`](shell-quality-w1-tooling-and-policy-tests.md) | Pinned provisioner, ShellCheck/shfmt config, `shell-quality.sh` runner, Make targets, failing policy tests, SC1091 baseline cleanup |
| W2 | [`shell-quality-w2-shfmt-baseline.md`](shell-quality-w2-shfmt-baseline.md) | Isolated formatting-only reformat of every tracked `.sh` |
| W3 | [`shell-quality-w3-strict-mode-classifier.md`](shell-quality-w3-strict-mode-classifier.md) | `check-shell-policy.sh` + exception manifest; de-leak the two sourced libs |
| W4 | [`shell-quality-w4-universal-enforcement.md`](shell-quality-w4-universal-enforcement.md) | Wire `shell-check` into the gauntlet + CI; actionlint→pinned ShellCheck; changed-file fast path |
| W5 | [`shell-quality-w5-composite-action-extraction.md`](shell-quality-w5-composite-action-extraction.md) | Extract `verify-oci-tfstate-creds` (and the next-largest) inline shell into gated `.sh` + stub tests |
| W6 | [`shell-quality-w6-critical-behavioral-tests.md`](shell-quality-w6-critical-behavioral-tests.md) | Offline command-stub tests for installer, bastion, deploy/rollback/smoke, Loki transports |
| W7 | [`shell-quality-w7-docs-and-migration-heuristic.md`](shell-quality-w7-docs-and-migration-heuristic.md) | `/docs` canonical commands + strict-mode classes; Bash-vs-Go migration heuristic; archive |

## Sequencing Notes

- This effort precedes resuming the `fast-path-*` work and
  `context-driven-fault-injection.md`.
- Pending documentation-doctrine hook edits land before W2 so the formatting baseline
  does not collide with hook changes; otherwise W2's reformat would re-touch
  hook files mid-flight. Coordinate, do not silently reformat over pending hook
  work.
