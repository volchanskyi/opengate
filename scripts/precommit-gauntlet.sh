#!/usr/bin/env bash
# precommit-gauntlet.sh — single source of truth for the precommit checks.
#
# Runs EVERY mandatory check from .claude/skills/precommit/SKILL.md in order.
# Invoked by:
#   - the /precommit skill (informational; same checks, same exits)
#   - .claude/hooks/pretooluse-git-commit-guard.sh (enforcement; the hook is
#     the gate, no marker bypass possible)
#
# Exit 0 = all checks passed. Exit 1 = a check failed (the failing check's
# output is printed to stderr above the exit). Exit 2 = prerequisite missing
# (Postgres not reachable, SONAR_TOKEN absent, etc.) — also blocks the
# commit; prerequisites must be fixed, not skipped.
#
# Environment:
#   POSTGRES_TEST_URL  — required for Go DB-dependent tests + coverage.
#   SONAR_TOKEN        — required for SonarCloud. Sourced from .env if present.
#   PRECOMMIT_SKIP_BENCH=1 — opt out of benchmarks for fast iteration on
#                            non-perf-touching commits. Use sparingly.
#
# NO bypass for tests / lint / e2e / sonar. Those are unconditional.
# Sonar is always the FULL `make sonar` (includes fresh coverage upload) so a
# coverage regression cannot slip past local enforcement and surface only in CI.

set -uo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT" || exit 2

# Source .env so SONAR_TOKEN and similar local-only secrets are available.
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

START_EPOCH="$(date +%s)"
FAIL_COUNT=0
FAILED_STEPS=()

color() {
  if [ -t 2 ]; then
    printf '\033[%sm' "$1" >&2
  fi
}
banner()  { color "1;36"; printf '\n=== %s ===\n' "$1" >&2; color "0"; }
running() { color "1;34"; printf '▶ %s\n' "$1" >&2; color "0"; }
ok()      { color "1;32"; printf '✓ %s (%ds)\n' "$1" "$2" >&2; color "0"; }
fail()    { color "1;31"; printf '✗ %s (%ds)\n' "$1" "$2" >&2; color "0"; }

# run_check NAME -- CMD ARGS...
# Captures output; on failure, prints the captured output then continues
# (so the user sees ALL failures in one pass instead of fixing one then
# discovering the next).
run_check() {
  local name="$1"; shift
  [ "$1" = "--" ] && shift
  local start; start="$(date +%s)"
  running "$name"
  local tmpfile; tmpfile="$(mktemp)"
  if "$@" >"$tmpfile" 2>&1; then
    ok "$name" "$(( $(date +%s) - start ))"
    rm -f "$tmpfile"
    return 0
  fi
  local rc=$?
  fail "$name" "$(( $(date +%s) - start ))"
  # Show the last 80 lines of output — full log is at $tmpfile path.
  tail -80 "$tmpfile" >&2 || true
  printf '  (full log: %s, exit code: %s)\n' "$tmpfile" "$rc" >&2
  FAIL_COUNT=$((FAIL_COUNT + 1))
  FAILED_STEPS+=("$name")
  return 0  # keep going so all failures surface in one pass
}

# Prerequisites first — if missing, we cannot validly enforce the gate.
banner "Prerequisites"

if [ -d "$HOME/go/src/net" ] || [ -f "$HOME/go/VERSION" ]; then
  color "1;31"
  echo "✗ \$HOME/go appears to be a Go install root, which collides with the default GOPATH." >&2
  echo "  Remove the manual install (\`rm -rf \$HOME/go\`) and use a snap/apt-managed Go binary." >&2
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

# Postgres reachability gate (deterministic). $POSTGRES_TEST_URL being set
# is not enough — the test DB has to actually accept connections, otherwise
# every DB-dependent Go test fails with "connection refused" and the rest
# of the gauntlet wastes 10+ minutes downstream.
#
# Implementation lives in scripts/lib/postgres-prereq.sh so it can be
# unit-tested via scripts/tests/postgres-prereq.test.sh. pg_ensure_up
# auto-starts the container when unreachable, waits up to 30s for
# readiness, and exits non-zero if it still can't connect — fail-loud
# per CLAUDE.md "no silent skip" rule.
# shellcheck source=lib/postgres-prereq.sh
. "$PROJECT_ROOT/scripts/lib/postgres-prereq.sh"
if ! pg_ensure_up; then
  color "1;31"
  echo "✗ Postgres prerequisite gate failed. See messages above." >&2
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

# Semgrep — required by the ADR-027 pen-test gate. Fail loud with the install
# command (no silent skip per .claude/rules/editing-and-scope.md).
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

# pmat — required by the ADR-019 TDG gate (the exact-version pin is enforced
# by scripts/pmat-precommit.sh; here we only check presence, fail-loud).
if ! command -v pmat >/dev/null 2>&1; then
  color "1;31"
  echo "✗ pmat is not installed — the ADR-019 TDG gate cannot run." >&2
  echo "  Install the pinned version (ADR-019 §5.5):" >&2
  echo "    cargo install --locked --version 3.17.0 pmat" >&2
  color "0"
  exit 2
fi

echo "✓ all prerequisites present" >&2

# Phase 1: lints (fast, fail-fast for cheap signal).
banner "Lints"
run_check "rust fmt"          -- bash -c 'cd agent && cargo fmt --all -- --check'
run_check "rust clippy"       -- bash -c 'cd agent && cargo clippy --workspace -- -D warnings'
run_check "go fmt"            -- bash -c '
  cd server
  unformatted=$(gofmt -l .)
  if [ -n "$unformatted" ]; then
    echo "::error::gofmt: files not formatted. Fix: cd server && gofmt -w ."
    printf "%s\n" "$unformatted"
    exit 1
  fi
'
run_check "go vet"            -- bash -c 'cd server && go vet ./...'
run_check "go-arch-lint"      -- bash -c 'cd server && go-arch-lint check'
run_check "cargo modules"     -- bash -c '
  cd agent
  actual=$(NO_COLOR=1 cargo modules structure --no-fns --no-types --no-traits --package mesh-agent-core 2>&1)
  if ! printf "%s\n" "$actual" | diff -u crates/mesh-agent-core/tests/module-graph.snap - ; then
    echo "::error::mesh-agent-core module graph diverged from the ADR-020 §5.2 snapshot."
    echo "Review the diff above. If the change is intentional, regenerate:"
    echo "  cd agent && NO_COLOR=1 cargo modules structure --no-fns --no-types --no-traits --package mesh-agent-core > crates/mesh-agent-core/tests/module-graph.snap"
    exit 1
  fi
'
run_check "cargo-deny"        -- bash -c 'cd agent && cargo-deny check --hide-inclusion-graph 2>&1'
run_check "web eslint"        -- bash -c 'cd web && npx eslint .'
run_check "depcruise"         -- bash -c '
  cd web
  current=$(npx --no-install depcruise src --output-type json --no-progress 2>/dev/null | jq -r ".summary.warn")
  baseline=$(jq -r ".warn" dependency-cruiser.snapshot.json)
  if [ -f ../.claude/.markers/arch-lint-flipped/depcruise ]; then
    # ADR-020 §5.4 flipped: zero is the only allowed count.
    if [ "$current" -gt 0 ]; then
      echo "::error::depcruise (flipped to error mode) violations: current=$current (ADR-020 §5.3+§5.4)."
      exit 1
    fi
  else
    if [ "$current" -gt "$baseline" ]; then
      echo "::error::depcruise warning count grew: current=$current baseline=$baseline (ADR-020 §5.3)."
      echo "Either fix the new violation or, if intentional, update the baseline:"
      echo "  jq \".warn = $current\" web/dependency-cruiser.snapshot.json > /tmp/snap.json && mv /tmp/snap.json web/dependency-cruiser.snapshot.json"
      exit 1
    fi
  fi
'
run_check "actionlint"        -- bash -c 'actionlint'
run_check "taint (go)"        -- make taint-go
run_check "taint (web)"       -- make taint-web
# ADR-027 adversarial pen-test gate. Diff vs origin/dev so a local dev-push
# gauntlet re-run is not blocked by pre-existing grandfathered findings.
run_check "pentest-review"    -- bash -c 'PENTEST_BASELINE_REF=origin/dev scripts/pentest-review.sh'
run_check "dead-code"         -- make dead-code
run_check "gitleaks (staged)" -- gitleaks protect --staged --config .gitleaks.toml --no-banner --redact
run_check "lint-deploy"       -- make lint-deploy

# Phase 2: codegen sync — would be a CI failure otherwise.
banner "Codegen sync"
run_check "verify-codegen"    -- bash -c "PATH=\"\$HOME/go/bin:\$PATH\" make verify-codegen"

# Phase 3: tests (the meat).
banner "Tests"
# Shell tests for CI gates / hooks / helper scripts (scripts/tests/*.test.sh).
# Iterate by glob so adding a new test file requires no gauntlet edit.
run_check "shell tests"        -- bash -c '
  rc=0
  shopt -s nullglob
  for t in scripts/tests/*.test.sh; do
    if [ ! -x "$t" ]; then
      echo "not executable: $t" >&2
      rc=1
      continue
    fi
    echo "▶ $t"
    if ! "$t"; then
      echo "✗ $t failed" >&2
      rc=1
    fi
  done
  exit $rc
'
run_check "go unit + coverage" -- bash -c '
  cd server && go test -race -count=1 -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...
'
run_check "go integration"     -- bash -c 'cd server && go test -race -count=1 -timeout 5m ./tests/...'
run_check "rust tests"         -- bash -c 'cd agent && cargo test --workspace'
run_check "web vitest+cov"     -- bash -c 'cd web && npx vitest run --coverage'

# Phase 4: coverage thresholds (derived from artifacts above).
banner "Coverage thresholds"
# shellcheck disable=SC2016 # $pct is set and consumed inside the inner shell; outer expansion is not desired.
run_check "go coverage ≥80%"   -- bash -c '
  cd server
  grep -v -E "/(testutil|metrics|amt/transport/wsman)/|api/openapi_gen\.go" coverage.out > coverage-prod.out
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
run_check "rust coverage ≥80%" -- bash -c '
  cd agent && cargo llvm-cov nextest --workspace --fail-under-lines 80 \
    --ignore-filename-regex "(main\.rs|/webrtc\.rs|/terminal\.rs|/session/mod\.rs|/session/relay\.rs|/tests/)"
'

# Phase 5: security audits — lockfile-based; fail on any reported vuln.
banner "Security audits"
run_check "govulncheck"        -- bash -c 'cd server && govulncheck ./...'
run_check "npm audit"          -- bash -c 'cd web && npm audit --audit-level=high'
run_check "cargo audit"        -- bash -c 'cd agent && cargo audit'
run_check "cargo deny"         -- bash -c 'cd agent && cargo deny check 2>&1'

# Phase 6: benchmarks — must run without errors (no perf thresholds enforced).
if [ "${PRECOMMIT_SKIP_BENCH:-0}" = "1" ]; then
  banner "Benchmarks (SKIPPED via PRECOMMIT_SKIP_BENCH=1)"
else
  banner "Benchmarks"
  run_check "go benchmarks"    -- bash -c 'cd server && go test -bench=. -benchmem -count=1 -run="^$" ./internal/...'
  run_check "rust benchmarks"  -- bash -c 'cd agent && cargo bench -p mesh-protocol'
fi

# Phase 7: end-to-end + SonarCloud (the slowest).
banner "E2E"
run_check "make e2e"           -- make e2e

banner "SonarCloud"
# Always the full scan with fresh coverage upload. `sonar-quick` is intentionally
# not wired in: a quality-gate evaluation against stale coverage was the gap
# that let new_coverage regressions reach CI undetected.
run_check "make sonar"         -- make sonar

# Phase 8: PMAT TDG gate (ADR-019 §"Integration point 2" / ADR-028). Appended
# last so it never masks faster checks. Grades ONLY changed code files at the
# B+ floor (Clean-as-You-Code) and passes trivially on docs-only / CI-only
# commits. Wrapper owns the changed-file resolution + exact-version pin.
banner "PMAT TDG gate"
run_check "pmat tdg ≥ B+ (changed code)" -- bash scripts/pmat-precommit.sh

# Summary.
ELAPSED=$(( $(date +%s) - START_EPOCH ))
banner "Summary"
if [ "$FAIL_COUNT" -eq 0 ]; then
  color "1;32"
  printf 'ALL CHECKS PASSED in %ds\n' "$ELAPSED" >&2
  color "0"
  exit 0
fi

color "1;31"
printf '%d CHECK(S) FAILED in %ds:\n' "$FAIL_COUNT" "$ELAPSED" >&2
for s in "${FAILED_STEPS[@]}"; do
  printf '  ✗ %s\n' "$s" >&2
done
color "0"
exit 1
