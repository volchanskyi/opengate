# SonarCloud Workflow

**Enforced by:** [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh) (suppression-string ban). **No bypass.**

When a quality-gate failure lands on `dev`, fix **all** findings in **one**
commit.

## Local scan

- The precommit gauntlet always runs `make sonar` — full scan with fresh
  coverage upload.
- `make sonar-quick` is for ad-hoc probing only. The gauntlet does not use it,
  and no `PRECOMMIT_SKIP_SONAR` escape hatch exists.
- Requires Docker and `SONAR_TOKEN` in the environment or in `.env`
  (gitignored). Generate a User Token at sonarcloud.io/account/security scoped
  to the `volchanskyi` organization.

### A green local scan is not a green gate on its own

Every `new_*` condition is scoped by git blame, and the gauntlet scans before
the commit exists. Three guards run after `make sonar`, each reading a measure
computed from file content rather than from blame:

| Guard | Gate condition it stands in for |
|---|---|
| [`sonar-coverage-guard.sh`](../../scripts/sonar-coverage-guard.sh) | `new_coverage`, held off the 80.0 boundary by a buffer, plus every line the diff touched |
| [`sonar-duplication-guard.sh`](../../scripts/sonar-duplication-guard.sh) | `new_duplicated_lines_density`, per changed file |
| [`sonar-rating-guard.sh`](../../scripts/sonar-rating-guard.sh) | `new_reliability_rating`, `new_security_rating`, `new_security_hotspots_reviewed`, per changed file |

- The coverage guard asks SonarCloud for the per-line hits it computed, and
  falls back to the coverage reports the scan uploaded. Neither source answering
  is a refusal.
- The rating guard fails on a bug, vulnerability or unreviewed hotspot on
  changed **main** code, and reports without failing on findings that move no
  gate condition. A finding on an untouched file does not fail the commit.

### A red local scan is not a red gate either

- On SonarCloud `dev` is a short-lived branch, so its new code is everything
  since it left `main`, and the boundary is the merge base. The scanner resolves
  the reference branch by name, which finds the local `refs/heads/main`.
- [`sonar-reference-branch.sh`](../../scripts/lib/sonar-reference-branch.sh)
  runs in the gauntlet's prerequisite phase. It refuses a local `main` behind
  `origin/main` and prints `git fetch origin main:main`. A branch ahead of its
  remote passes.

## Fetch everything, not just issues

On the first failure, query all three endpoints in parallel. They return
disjoint data:

- `GET /api/issues/search?componentKeys=volchanskyi_opengate&branch=dev&resolved=false&inNewCodePeriod=true&ps=100` — **issues**
- `GET /api/hotspots/search?projectKey=volchanskyi_opengate&branch=dev&status=TO_REVIEW&inNewCodePeriod=true&ps=100` — **security hotspots**
- `GET /api/qualitygates/project_status?projectKey=volchanskyi_opengate&branch=dev` — which gate **conditions** failed

The issues endpoint returning `total: 0` does not mean the gate is green.

## Audit the diff for analogous patterns

Before pushing, grep the diff for patterns this project has fixed before.
`git log --oneline --grep=SonarCloud --grep=sonar -i` shows past fixes. On any
new Go DB file:

- `fmt\.Sprintf.*(SELECT|INSERT|UPDATE|DELETE|CREATE|DROP)` — `go:S2077`
- `strings\.Join.*(WHERE|AND|OR)` — same hotspot, different shape
- 3+ identical string literals in one file — `go:S1192`

## No suppression without approval

- Do **not** add Sonar inline-suppression comments, Go lint-suppression
  directives, the Sonar config-file multicriteria ignore entry, or ESLint
  disable annotations unless the user explicitly approves.
- Restructure the code so the pattern matcher is satisfied. Reference patterns:
  the existing fixes in `sqlite.go` and `postgres.go`.
- The write-guard hook blocks any `Write`/`Edit`/`MultiEdit` whose new content
  contains the banned suppression strings.
