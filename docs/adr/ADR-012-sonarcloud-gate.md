---
number: 12
title: The SonarCloud gate blocks the merge
---

# ADR-012 — The SonarCloud gate blocks the merge

## Context

Per-language thresholds alone miss what SonarCloud measures and `go test -cover`
does not — duplication, cognitive complexity, security hotspots. In GitHub Code
Scanning, fingerprint matching treated dismissed findings as already seen and
kept new ones invisible.

## Decision

A required CI job aggregates coverage from the Go, Rust and web test jobs, runs
the pinned scanner over the whole codebase, and waits for the quality gate to
resolve. Findings live in the SonarCloud console.

The gate's conditions apply to new code only: coverage at or above 80%, an A
rating for reliability, security and maintainability, every new security hotspot
reviewed, and duplication under 3%.

Overall coverage is held separately, by each language's own test job failing
below 80%, and the pre-commit run reproduces all of it. So three independent
layers together hold both the whole codebase and each change to it, even though
the gate itself only measures the change.

Repeated code is held the same way. The scan keeps its report, and every
production file in the codebase must hold at most 3% repeated lines, read from
the scanner's own result
([ADR-125](ADR-125-comments-and-repeated-code.md)). The scanner is pinned
in the [tool-version manifest](../../scripts/lib/tool-versions.sh).

A local scan stands in for the gate only with three guards beside it.
SonarCloud raises issues only on committed content, so `make sonar` scans a
snapshot commit of the work tree, sitting on `HEAD`, from a throwaway worktree
([`sonar-scan.sh`](../../scripts/sonar-scan.sh)), and brings the scan's kept report
back. Every `new_*` condition is scoped by git blame, so each guard reads a
measure computed from file content instead:

| Guard | Gate condition it stands in for |
|---|---|
| [`sonar-coverage-guard.sh`](../../scripts/sonar-coverage-guard.sh) | `new_coverage`, held off the 80.0 boundary by a buffer, plus every line the diff touched |
| [`check-duplication`](../../scripts/check-duplication/) | `new_duplicated_lines_density`, widened to every production file in the codebase |
| [`sonar-rating-guard.sh`](../../scripts/sonar-rating-guard.sh) | `new_reliability_rating`, `new_security_rating`, `new_security_hotspots_reviewed`, per changed file |

The coverage guard asks SonarCloud for the per-line hits it computed and falls
back to the coverage reports the scan uploaded; neither answering is a refusal.
Those hits count only where SonarCloud's copy of the file is the working tree's,
since the branch holds whichever analysis finished last. The rating guard fails
on a bug, vulnerability or unreviewed hotspot on changed main code and reports
the findings that move no gate condition; a finding on an untouched file fails
nothing. The duplication reader refuses a report it cannot read, one holding no
production files, and a missing one, and the CI `sonarcloud` job runs it after
its own scan.

CI reads the gate's own answer. A CI scan that reached SonarCloud leaves
`.scannerwork/report-task.txt`, so its failure is the gate's verdict and fails
the job as one; only a scan that never reached SonarCloud hands over to the
Docker fallback, which runs as the runner's user and names the branch or pull
request from the event.
[`sonarcloud-main.yml`](../../.github/workflows/sonarcloud-main.yml) analyses
`main` after every merge, with the coverage of the CI run that tested it, so
`dev` is compared against the code that shipped; it gates nothing.

## Consequences

Running SonarCloud as a hard gate forces fixes on `dev` rather than letting them
pile up on `main`.

A red local scan can describe a stale reference. On SonarCloud `dev` is a
short-lived branch, so its new code is everything since the merge base with
`main`, and the scanner finds the reference branch by name — the local
`refs/heads/main`.
[`sonar-reference-branch.sh`](../../scripts/lib/sonar-reference-branch.sh) refuses
a local `main` behind `origin/main` in the gauntlet's prerequisite phase, and a
branch ahead of its remote passes.
