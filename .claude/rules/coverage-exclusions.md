# Coverage Exclusions and Issue Suppressions Are a Last Resort

**Enforced by:** [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh)
(suppression-string ban),
[`scripts/sonar-coverage-exclusion-guard.sh`](../../scripts/sonar-coverage-exclusion-guard.sh)
(justification, list agreement, split inheritance, staleness). **No bypass.**

Companion to [`sonarcloud.md`](sonarcloud.md), which governs *issue*
suppressions. This rule governs *coverage* exclusions and states the standard
both share.

## The rule

A failing quality gate is fixed by **writing the test** or **restructuring the
code**. Excluding the file and suppressing the finding are the last options
considered, never the first, and both need explicit user approval.

Reaching for an exclusion is a claim that the code **cannot be executed by an
in-process test at all**. That is a strong, falsifiable claim about a harness,
not a statement about effort. These are reasons:

- a binary entry point — the process's own `main`, which a test cannot enter;
- a live network stack the test host cannot stand up (STUN/ICE negotiation);
- a PTY plus a real shell subprocess;
- a loop driven by a live screen source;
- generated code, regenerated from a spec that is itself reviewed;
- test scaffolding, which is not production code.

These are **not** reasons, and an entry resting on one of them is removed:

- "it is hard to test", "it is mostly IO", "it is transport-ish";
- "it is covered by integration tests" — if an integration test covers it, wire
  that coverage into the report instead of exempting the file;
- "the whole package is infrastructure" — a package is never the unit; a
  directory glob hides every file later added under it, including files nobody
  decided to exempt;
- "it was excluded before" — an inherited entry is re-earned, not grandfathered.

## Every entry carries its own justification

An exclusion whose reason is not written next to it cannot be reviewed and will
never be removed. Each entry in
[`sonar-project.properties`](../../sonar-project.properties) carries a comment
naming **which** of the admissible reasons applies to **that** file — the same
convention the mutation carve-outs already follow
([`docs/infrastructure/Testing.md`](../../docs/infrastructure/Testing.md), `agent/.cargo/mutants.toml`).

The guard fails the gauntlet on any entry without one.

## Name files, never directories

Every exclusion names a single file. A `**/dir/**` glob silently exempts every
file added under it afterwards, so the list stops describing a set of decisions
and starts describing a place where coverage does not apply. The two glob shapes
that remain are test scaffolding and generated output, neither of which is
production code.

## The lists must agree

Coverage is enforced in four places, and an exclusion added to one is invisible
in the others:

| Where | What it holds |
|---|---|
| [`sonar-project.properties`](../../sonar-project.properties) | `sonar.coverage.exclusions` — the new-code gate |
| [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) | the per-language ≥80% jobs |
| [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) | the same per-language checks locally |
| [`Makefile`](../../Makefile)'s `sonar-coverage` | the report the gate itself reads |

A path exempt in all of them is checked by nothing at all, which is how a package
carrying more test code than production code came to be measured by no gate. The
guard fails when the Rust ignore lists disagree.

The fourth row is the one that hides: it generates what SonarCloud reads, so a
list that grows there narrows the gate's view of the workspace while both ≥80%
jobs go on measuring the wider one, and no number anywhere changes to say so. It
had drifted by four files.

## The report has to be readable by the scanner that reads it

An exclusion is a decision. A report the analysis cannot resolve is the same
outcome reached by accident, and it is the harder one to notice: nothing is
listed anywhere, so there is nothing to review.

`cargo llvm-cov` names every file by its absolute path on the machine that ran
it. Every scanner that reads the report analyses the tree from inside a
container mounted somewhere else — `/usr/src` for the Docker scanner, and
`/github/workspace` for the scan action CI runs — so those paths resolve to no
indexed file and the coverage is dropped without a word. No Rust file had ever
carried a coverage figure, for the life of the project, while two ≥80% jobs
passed against that very report.

So the report is rewritten into the reader's coordinates and then asked whether
it worked: [`rust-lcov-relativize.sh`](../../scripts/rust-lcov-relativize.sh)
fails on a report that still names an absolute path, and on one that names no
source at all. The rewrite is not the guard — the read-back is, for the reason
[`ci-cd-determinism.md`](ci-cd-determinism.md) gives.

## An exclusion is re-earned, not kept

Code changes; a file that gained a harness no longer qualifies. When the gate is
touched for any reason, an entry whose file now clears the threshold is deleted
in that same commit. The guard reports every entry's current coverage so this is
a fact rather than a guess.

## A split inherits its parent's exclusion

Carving a new file out of an excluded one gives the relocated code a path that
nothing exempts, and git blame dates every moved line to the split — so it lands
in the new-code gate at whatever coverage it has, and the pre-commit scan cannot
see it because blame has no commit for those lines yet. The guard fails the
split unless the new path is excluded too, or is tested.
