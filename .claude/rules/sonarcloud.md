# SonarCloud Workflow

**Enforced by:** [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh) (suppression-string ban). **No bypass.**

When a SonarCloud quality-gate failure lands on `dev`, fix **all** findings in **one** commit. Never push → wait → fix-one → push-again — each round trip wastes a CI cycle and fragments git history.

## Local SonarCloud scan

The precommit gauntlet always runs `make sonar` (full scan with fresh coverage upload). `make sonar-quick` exists in the Makefile for ad-hoc code-quality probing, but the gauntlet does **not** use it — a quality-gate evaluation against stale coverage previously let a `new_coverage` regression surface only in CI. No `PRECOMMIT_SKIP_SONAR` escape hatch exists.

Requires Docker and `SONAR_TOKEN` set in the environment or in `.env` (gitignored). Generate a User Token at sonarcloud.io/account/security scoped to the `volchanskyi` organization.

## Fetch everything, not just issues

On the **first** failure, query all three SonarCloud endpoints in parallel. They return disjoint data:

- `GET /api/issues/search?componentKeys=volchanskyi_opengate&branch=dev&resolved=false&inNewCodePeriod=true&ps=100` — code-smell / bug / vulnerability **issues**
- `GET /api/hotspots/search?projectKey=volchanskyi_opengate&branch=dev&status=TO_REVIEW&inNewCodePeriod=true&ps=100` — **security hotspots** (separate endpoint, separate data)
- `GET /api/qualitygates/project_status?projectKey=volchanskyi_opengate&branch=dev` — which gate **conditions** failed (hotspot-review %, coverage, duplication, ratings)

The issues endpoint returning `total: 0` does **not** mean the gate is green. Hotspots and coverage live elsewhere. Always pull all three before deciding what to fix.

## Audit the diff for analogous patterns

Before pushing, grep the diff for patterns the project has fixed before. `git log --oneline --grep=SonarCloud --grep=sonar -i` shows past fixes — when a new file is added alongside an existing fixed file (e.g. `postgres.go` next to `sqlite.go`), the new file **will** re-introduce the same rule violations unless you search for them. Concrete patterns to grep on any new Go DB file:

- `fmt\.Sprintf.*(SELECT|INSERT|UPDATE|DELETE|CREATE|DROP)` — `go:S2077` dynamic SQL hotspot
- `strings\.Join.*(WHERE|AND|OR)` — same hotspot, different shape
- 3+ identical string literals in one file — `go:S1192` duplicated literals

## No suppression without approval

Do **not** add Sonar inline-suppression comments, Go lint-suppression directives, the Sonar config-file multicriteria ignore entry, or ESLint disable annotations to clear the gate unless the user explicitly approves. Restructure the code so the linter's pattern matcher is satisfied — the existing fixes in `sqlite.go` and `postgres.go` are reference patterns to copy.

The write-guard hook ([`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh)) blocks any `Write`/`Edit`/`MultiEdit` whose new content contains the banned suppression strings. See the hook source for the exact regex patterns.
