# SonarCloud Workflow

**Enforced by:** [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh) (suppression-string ban). **No bypass.**

When a quality-gate failure lands on `dev`, fix **all** findings in **one**
commit.

## Local scan

- The precommit gauntlet always runs `make sonar` — full scan with fresh
  coverage upload.
- `make sonar` scans a snapshot commit of the work tree from a throwaway worktree
  ([`sonar-scan.sh`](../../scripts/sonar-scan.sh)), and three guards run after it:
  [`sonar-coverage-guard.sh`](../../scripts/sonar-coverage-guard.sh),
  [`check-duplication`](../../scripts/check-duplication/) and
  [`sonar-rating-guard.sh`](../../scripts/sonar-rating-guard.sh). A commit they refuse is
  refused whatever the scan reported. Why a local scan needs them:
  [ADR-012](../../docs/adr/ADR-012-sonarcloud-gate.md).
- A local `main` behind `origin/main` is refused before the scan; run
  `git fetch origin main:main`.
- `make sonar-quick` is for ad-hoc probing only. The gauntlet does not use it,
  and no `PRECOMMIT_SKIP_SONAR` escape hatch exists.
- Requires Docker and `SONAR_TOKEN` in the environment or in `.env`
  (gitignored). Generate a User Token at sonarcloud.io/account/security scoped
  to the `volchanskyi` organization.

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
- a statement constant joined with `+` and handed straight to a query call —
  `go:S2077`, even when every piece is constant; one complete literal is not
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
