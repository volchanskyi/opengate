# Coverage Exclusions and Issue Suppressions Are a Last Resort

**Enforced by:**
[`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh)
(suppression-string ban),
[`scripts/sonar-coverage-exclusion-guard.sh`](../../scripts/sonar-coverage-exclusion-guard.sh)
(justification, list agreement, split inheritance, staleness). **No bypass.**

Companion to [`sonarcloud.md`](sonarcloud.md), which governs *issue*
suppressions. This governs *coverage* exclusions and the standard both share.

## The rules

A failing quality gate is fixed by writing the test or restructuring the code.
Excluding the file and suppressing the finding are the last options considered,
and both need explicit user approval.

### An exclusion is a claim that the code cannot be executed by an in-process test

Admissible reasons:

- a binary entry point — the process's own `main`;
- a live network stack the test host cannot stand up (STUN/ICE negotiation);
- a PTY plus a real shell subprocess;
- a loop driven by a live screen source;
- generated code, regenerated from a reviewed spec;
- test scaffolding.

Not reasons, and an entry resting on one is removed:

- "it is hard to test", "it is mostly IO", "it is transport-ish";
- "it is covered by integration tests" — wire that coverage into the report;
- "the whole package is infrastructure" — a package is never the unit;
- "it was excluded before".

### Every entry carries its own justification

- Each entry in
  [`sonar-project.properties`](../../sonar-project.properties) carries a comment
  naming which admissible reason applies to that file.
- The guard fails the gauntlet on any entry without one.

### Name files, never directories

- Every exclusion names a single file.
- The only glob shapes that remain are test scaffolding and generated output.

### The lists must agree

| Where | What it holds |
|---|---|
| [`sonar-project.properties`](../../sonar-project.properties) | `sonar.coverage.exclusions` — the new-code gate |
| [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) | the per-language ≥80% jobs |
| [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) | the same per-language checks locally |
| [`Makefile`](../../Makefile)'s `sonar-coverage` | the report the gate itself reads |

- The guard fails when the Rust ignore lists disagree.

### The report has to be readable by the scanner that reads it

- The report is rewritten into the reader's coordinates and then asked whether
  it worked.
- [`rust-lcov-relativize.sh`](../../scripts/rust-lcov-relativize.sh) fails on a
  report that still names an absolute path, and on one that names no source at
  all.

### An exclusion is re-earned, not kept

- When the gate is touched for any reason, an entry whose file now clears the
  threshold is deleted in that same commit.
- The guard reports every entry's current coverage.

### A split inherits its parent's exclusion

- The guard fails a file carved out of an excluded one unless the new path is
  excluded too, or is tested.
