#!/usr/bin/env bash
# Every source a profile limits is a generator the workflow naming that profile runs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOW_DIR="$REPO_ROOT/.github/workflows"
PROFILE_DIR="$REPO_ROOT/load/profiles"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$REPO_ROOT/scripts/lib/loadtest-profile.sh"

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

echo "loadtest gate venue:"

workflows_naming() {
  grep -rlE "load/profiles/$1\.yaml" "$WORKFLOW_DIR" 2>/dev/null || true
}

# The k6 source is the browser side; the quic source is the machine-side harness binary.
sources_run_by() {
  local workflow="$1"
  grep -qE 'loadtest-k6-run\.sh|k6 run' "$workflow" && echo k6
  grep -qE '/tmp/loadtest\b|loadtest-quic-(run|incluster)\.sh' "$workflow" && echo quic
  return 0
}

checked=0
for profile in "$PROFILE_DIR"/*.yaml; do
  name="$(basename "$profile" .yaml)"
  naming="$(workflows_naming "$name")"

  [ -n "$naming" ] || continue

  available="$(
    while IFS= read -r workflow; do
      [ -n "$workflow" ] || continue
      sources_run_by "$workflow"
    done <<<"$naming" | sort -u
  )"

  if [ -z "$available" ]; then
    fail "$name is named by a workflow that starts no generator at all"
    continue
  fi

  bad=""
  while IFS= read -r series; do
    [ -n "$series" ] || continue
    source="${series%%/*}"
    grep -qxF "$source" <<<"$available" || bad="$bad $series"
  done < <(profile_gates "$profile" | jq -r '.[].series' | sort -u)

  checked=$((checked + 1))
  if [ -z "$bad" ]; then
    pass "$name limits only what its venue produces ($(echo "$available" | tr '\n' ' '))"
  else
    fail "$name limits measurements its venue cannot produce:$bad (it runs: $(echo "$available" | tr '\n' ' '))"
  fi
done

if [ "$checked" -ge 2 ]; then
  pass "reached $checked profiles that a workflow names"
else
  fail "reached only $checked profiles — no workflow names a profile any more, or the naming changed shape"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
