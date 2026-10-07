#!/usr/bin/env bash
# Runs every mandatory precommit check in order and records the content it passed on success.
# The commit guard invokes it, and it runs by hand to check without committing.
#
# Environment:
#   POSTGRES_TEST_URL  (required) the Postgres the Go database tests use
#   SONAR_TOKEN        (required) the SonarCloud token, sourced from .env when present
#   PRECOMMIT_SKIP_BENCH  1 skips the benchmarks
#
# Exit codes:
#   0  all checks passed
#   1  a check failed
#   2  prerequisite missing

set -uo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT" || exit 2

if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

# The guard gives every docker step a config whose broken credential helper is removed.
DOCKER_CONFIG="$(./scripts/docker-credstore-guard.sh)"
export DOCKER_CONFIG

# shellcheck source=../.claude/hooks/lib/tidy-up.sh
source "$PROJECT_ROOT/.claude/hooks/lib/tidy-up.sh"
TIDY_START="$(tidy_fingerprint || true)"

START_EPOCH="$(date +%s)"
FAIL_COUNT=0
FAILED_STEPS=()

color() {
  if [ -t 2 ]; then
    printf '\033[%sm' "$1" >&2
  fi
}
banner() {
  color "1;36"
  printf '\n=== %s ===\n' "$1" >&2
  color "0"
}
running() {
  color "1;34"
  printf '▶ %s\n' "$1" >&2
  color "0"
}
ok() {
  color "1;32"
  printf '✓ %s (%ds)\n' "$1" "$2" >&2
  color "0"
}
fail() {
  color "1;31"
  printf '✗ %s (%ds)\n' "$1" "$2" >&2
  color "0"
}

# run_check captures a command's output and, on failure, prints it and continues to the next check.
run_check() {
  local name="$1"
  shift
  [ "$1" = "--" ] && shift
  local start
  start="$(date +%s)"
  running "$name"
  local tmpfile
  tmpfile="$(mktemp)"
  if "$@" >"$tmpfile" 2>&1; then
    ok "$name" "$(($(date +%s) - start))"
    rm -f "$tmpfile"
    return 0
  fi
  local rc=$?
  fail "$name" "$(($(date +%s) - start))"
  tail -80 "$tmpfile" >&2 || true
  printf '  (full log: %s, exit code: %s)\n' "$tmpfile" "$rc" >&2
  FAIL_COUNT=$((FAIL_COUNT + 1))
  FAILED_STEPS+=("$name")
  return 0 # keep going so all failures surface in one pass
}

banner "Prerequisites"

if [ -d "$HOME/go/src/net" ] || [ -f "$HOME/go/VERSION" ]; then
  color "1;31"
  echo "✗ \$HOME/go appears to be a Go install root, which collides with the default GOPATH." >&2
  echo "  Remove the manual install (\`rm -rf \$HOME/go\`) and use a snap/apt-managed Go binary." >&2
  color "0"
  exit 2
fi

# CI resolves its floating toolchains at run time, so a drifted workstation fails the gate here.
# shellcheck source=lib/toolchain-parity.sh
. "$PROJECT_ROOT/scripts/lib/toolchain-parity.sh"
if ! toolchain_parity_check "$PROJECT_ROOT"; then
  color "1;31"
  echo "✗ Local toolchains are not the ones CI resolves — run the commands above and re-run." >&2
  color "0"
  exit 2
fi

if [ -z "${POSTGRES_TEST_URL:-}" ]; then
  color "1;31"
  echo "✗ POSTGRES_TEST_URL is unset — Postgres-dependent tests would skip silently." >&2
  echo "  Start the test DB and export:" >&2
  echo "    make postgres-test-up" >&2
  echo "    export POSTGRES_TEST_URL=\"postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable\"" >&2
  color "0"
  exit 2
fi

# pg_ensure_up starts the test container when Postgres is unreachable and fails if it stays down.
# shellcheck source=lib/postgres-prereq.sh
. "$PROJECT_ROOT/scripts/lib/postgres-prereq.sh"
if ! pg_ensure_up; then
  color "1;31"
  echo "✗ Postgres prerequisite gate failed. See messages above." >&2
  color "0"
  exit 2
fi

# The exported URL makes every Go test package share one store.
# shellcheck source=lib/victoriametrics-prereq.sh
. "$PROJECT_ROOT/scripts/lib/victoriametrics-prereq.sh"
if ! vm_ensure_up; then
  color "1;31"
  echo "✗ VictoriaMetrics prerequisite gate failed. See messages above." >&2
  color "0"
  exit 2
fi
export VICTORIAMETRICS_TEST_URL="${VICTORIAMETRICS_TEST_URL:-$(vm_test_url)}"

# The SonarCloud scan reads its reference branch by local name, so a stale local branch moves
# the new-code boundary.
# shellcheck source=lib/sonar-reference-branch.sh
. "$PROJECT_ROOT/scripts/lib/sonar-reference-branch.sh"
if ! sonar_reference_branch_check "$PROJECT_ROOT" main; then
  color "1;31"
  echo "✗ Reference-branch parity gate failed. See messages above." >&2
  color "0"
  exit 2
fi

if [ -z "${SONAR_TOKEN:-}" ]; then
  color "1;31"
  echo "✗ SONAR_TOKEN is unset — full SonarCloud scan is mandatory (no skip)." >&2
  echo "  Generate a User Token at sonarcloud.io/account/security (scope: volchanskyi)" >&2
  echo "  and add it to .env as SONAR_TOKEN=... or export it." >&2
  color "0"
  exit 2
fi

# The pen-test gate needs semgrep; a missing install exits 2 with the command that provisions it.
if ! command -v semgrep >/dev/null 2>&1; then
  export PATH="$HOME/.local/bin:$PATH"
fi
if ! command -v semgrep >/dev/null 2>&1; then
  color "1;31"
  echo "✗ semgrep is not installed — the ADR-027 pen-test gate cannot run." >&2
  echo "  Provision the pinned version (idempotent):" >&2
  echo "    bash scripts/install-semgrep.sh" >&2
  color "0"
  exit 2
fi

# The TDG gate needs pmat present; scripts/pmat-precommit.sh checks the exact version.
if ! command -v pmat >/dev/null 2>&1; then
  color "1;31"
  echo "✗ pmat is not installed — the ADR-019 TDG gate cannot run." >&2
  echo "  Install the pinned version (ADR-019):" >&2
  echo "    cargo install --locked --version 3.17.0 pmat" >&2
  color "0"
  exit 2
fi

# The docs Mermaid gate parses every fence with the pinned parser installed in node_modules.
if [ ! -d tools/mermaid-validate/node_modules ]; then
  color "1;31"
  echo "✗ tools/mermaid-validate/node_modules is missing — the Mermaid docs gate cannot run." >&2
  echo "  Provision the pinned parser once:" >&2
  echo "    (cd tools/mermaid-validate && npm ci --ignore-scripts)" >&2
  color "0"
  exit 2
fi

echo "✓ all prerequisites present" >&2

banner "Lints"
run_check "rust fmt" -- bash -c 'cd agent && cargo fmt --all -- --check'
run_check "rust clippy" -- bash -c 'cd agent && cargo clippy --workspace -- -D warnings'
# The variables below intentionally expand in the inner bash process.
# shellcheck disable=SC2016
run_check "go fmt" -- bash -c '
  cd server
  unformatted=$(gofmt -l .)
  if [ -n "$unformatted" ]; then
    echo "::error::gofmt: files not formatted. Fix: cd server && gofmt -w ."
    printf "%s\n" "$unformatted"
    exit 1
  fi
'
run_check "go vet" -- bash -c 'cd server && go vet ./...'
run_check "go-arch-lint" -- bash -c 'cd server && go-arch-lint check'
# The variables below intentionally expand in the inner bash process.
# shellcheck disable=SC2016
run_check "cargo modules" -- bash -c '
  cd agent
  actual=$(RUST_LOG=off NO_COLOR=1 cargo modules structure --no-fns --no-types --no-traits --package mesh-agent-core 2>&1)
  if ! printf "%s\n" "$actual" | diff -u crates/mesh-agent-core/tests/module-graph.snap - ; then
    echo "::error::mesh-agent-core module graph diverged from the ADR-020 snapshot."
    echo "Review the diff above. If the change is intentional, regenerate:"
    echo "  cd agent && NO_COLOR=1 cargo modules structure --no-fns --no-types --no-traits --package mesh-agent-core > crates/mesh-agent-core/tests/module-graph.snap"
    exit 1
  fi
'
run_check "cargo-deny" -- bash -c 'cd agent && cargo-deny check --hide-inclusion-graph 2>&1'
run_check "web eslint" -- bash -c 'cd web && npx eslint .'
# The web tsconfig is a solution file, so only a build-mode run checks the sources.
run_check "web typecheck" -- bash -c 'cd web && npx tsc -b'
run_check "depcruise" -- bash scripts/depcruise-check.sh
run_check "shell-check" -- make shell-check
run_check "actionlint" -- actionlint -shellcheck="$(command -v shellcheck)"
run_check "doc links" -- bash -c 'GO111MODULE=off go run ./scripts/check-doc-links'
run_check "mermaid syntax" -- bash -c 'cd tools/mermaid-validate && node validate-mermaid.mjs ../../docs'
run_check "taint (go)" -- make taint-go
run_check "taint (web)" -- make taint-web
# The pen-test gate diffs against origin/dev so only findings in the change count.
run_check "pentest-review" -- bash -c 'PENTEST_BASELINE_REF=origin/dev scripts/pentest-review.sh'
run_check "dead-code" -- make dead-code
run_check "gitleaks (staged)" -- gitleaks protect --staged --config .gitleaks.toml --no-banner --redact
run_check "lint-deploy" -- make lint-deploy
run_check "no-vm-ssh-guard" -- bash scripts/no-vm-ssh-guard.sh
# A file carved out of a coverage-excluded one is new code that `make sonar` cannot see pre-commit.
run_check "sonar coverage-exclusion guard" -- bash scripts/sonar-coverage-exclusion-guard.sh

banner "Codegen sync"
run_check "verify-codegen" -- bash -c "PATH=\"\$HOME/go/bin:\$PATH\" make verify-codegen"

banner "Tests"
# The shell tests run through the runner CI calls, which fails a test that writes to its step files.
run_check "shell tests" -- scripts/shell-quality.sh test
run_check "go unit + coverage" -- bash -c '
  cd server && go test -race -count=1 -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...
'
run_check "go integration" -- bash -c 'cd server && go test -race -count=1 -timeout 10m ./tests/...'
run_check "rust tests" -- bash -c 'cd agent && cargo test --workspace'
run_check "web vitest+cov" -- bash -c 'cd web && npx vitest run --coverage'

banner "Coverage thresholds"
# shellcheck disable=SC2016 # $pct is set and consumed inside the inner shell; outer expansion is not desired.
run_check "go coverage ≥80%" -- bash -c '
  cd server
  grep -v -E "/testutil/|api/openapi_gen\.go" coverage.out > coverage-prod.out
  pct="$(go tool cover -func=coverage-prod.out | awk "/^total:/ {gsub(\"%\", \"\", \$NF); print \$NF}")"
  awk -v p="$pct" "BEGIN { exit !(p+0 >= 80.0) }"
'
run_check "web coverage ≥80%" -- bash -c '
  cd web
  node -e "
    const s=require(\"./coverage/coverage-summary.json\");
    const l=s.total.lines.pct;
    console.log(\"Web line coverage: \"+l+\"%\");
    process.exit(l<80?1:0);
  "
'
# Only test files are ignored; every production path counts toward the threshold.
run_check "rust coverage ≥80%" -- bash -c '
  cd agent && cargo llvm-cov nextest --workspace --fail-under-lines 80 \
    --ignore-filename-regex "(/tests/)"
'

banner "Security audits"
# The script CI runs fetches the vulnerability database with retries and scans once.
run_check "govulncheck" -- bash scripts/govulncheck-scan.sh
# Each lockfile gets its own audit; the script CI runs applies the expiring exceptions.
run_check "npm audit (web)" -- bash scripts/npm-audit.sh web
run_check "npm audit (mermaid-validate)" -- bash scripts/npm-audit.sh tools/mermaid-validate
run_check "cargo audit" -- bash -c 'cd agent && cargo audit'
run_check "cargo deny" -- bash -c 'cd agent && cargo deny check 2>&1'

if [ "${PRECOMMIT_SKIP_BENCH:-0}" = "1" ]; then
  banner "Benchmarks (SKIPPED via PRECOMMIT_SKIP_BENCH=1)"
else
  banner "Benchmarks"
  run_check "go benchmarks" -- bash -c 'cd server && go test -bench=. -benchmem -count=1 -run="^$" ./internal/...'
  run_check "rust benchmarks" -- bash -c 'cd agent && cargo bench -p mesh-protocol'
fi

# The TDG gate runs before the slow phase because it is fast and fails often.
banner "PMAT TDG gate"
run_check "pmat tdg ≥ B+ (changed code)" -- bash scripts/pmat-precommit.sh

# The slow end-to-end and SonarCloud phase runs only when every earlier check passed.
if [ "$FAIL_COUNT" -gt 0 ]; then
  banner "E2E + SonarCloud (SKIPPED — $FAIL_COUNT earlier check(s) failed; fix those first)"
else
  banner "E2E"
  run_check "make e2e" -- make e2e

  banner "SonarCloud"
  # The full scan uploads fresh coverage so the gate never evaluates stale numbers.
  run_check "make sonar" -- make sonar
  # The guard holds aggregate coverage above a buffer over 80 and checks every changed line's hits.
  run_check "sonar new-coverage guard" -- bash scripts/sonar-coverage-guard.sh
  run_check "repeated code ≤ 3% per file" -- bash -c 'GO111MODULE=off go run ./scripts/check-duplication'
  # The guard checks the absolute findings on each changed file, which blame-based ratings miss.
  run_check "sonar new-rating guard" -- bash scripts/sonar-rating-guard.sh
fi

ELAPSED=$(($(date +%s) - START_EPOCH))
banner "Summary"
if [ "$FAIL_COUNT" -eq 0 ]; then
  color "1;32"
  printf 'ALL CHECKS PASSED in %ds\n' "$ELAPSED" >&2
  color "0"
  if tidy_gauntlet_passed "$TIDY_START"; then
    echo "Pass recorded for this content; /refactor may begin on it." >&2
  else
    echo "No pass recorded: the work tree changed while the checks ran, so what passed is not what is on disk. Run the gauntlet again before /refactor." >&2
  fi
  exit 0
fi

color "1;31"
printf '%d CHECK(S) FAILED in %ds:\n' "$FAIL_COUNT" "$ELAPSED" >&2
for s in "${FAILED_STEPS[@]}"; do
  printf '  ✗ %s\n' "$s" >&2
done
color "0"
exit 1
