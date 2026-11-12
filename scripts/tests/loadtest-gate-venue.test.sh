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

# jobs_naming prints the text of every job in a workflow that names the profile, or of every job
# when the workflow-level env names it, so a source counts only where the profile runs.
jobs_naming() {
  awk -v want="load/profiles/$2.yaml" '
    !injobs && /^jobs:/ { injobs = 1; all = (index(head, want) > 0); next }
    !injobs { head = head $0 "\n"; next }
    /^  [A-Za-z0-9_-]+:[[:space:]]*$/ {
      if (all || index(block, want) > 0) printf "%s", block
      block = ""
    }
    { block = block $0 "\n" }
    END { if (all || index(block, want) > 0) printf "%s", block }
  ' "$1"
}

# called_by prints the text of every workflow of this repository the jobs call.
called_by() {
  local path
  while IFS= read -r path; do
    [ -f "$REPO_ROOT/$path" ] && cat "$REPO_ROOT/$path"
  done < <(sed -n 's|^[[:space:]]*uses:[[:space:]]*\./\(\.github/workflows/[A-Za-z0-9_.-]*\)[[:space:]]*$|\1|p' <<<"$1" | sort -u)
  return 0
}

# The k6 source is the browser side; the quic source is the harness, whose catch-up client is the
# catchup source and its leak snapshots the leak source; a called leg walks catch-up when asked.
sources_run_by() {
  local jobs="$1" called
  called="$(called_by "$jobs")"
  grep -qE 'loadtest-k6-(run|alongside)\.sh|k6 run' <<<"$jobs$called" && echo k6
  grep -qE '/tmp/loadtest\b|loadtest-quic-(run|incluster)\.sh' <<<"$jobs$called" && echo quic
  grep -qE -- '-backfill-gap=|^[[:space:]]+catch-up:[[:space:]]*true[[:space:]]*$' <<<"$jobs" && echo catchup
  grep -qE -- '-leak-snapshot-every=' <<<"$jobs$called" && echo leak
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
      sources_run_by "$(jobs_naming "$workflow" "$name")"
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
    # The harness counts live readings on every walk, and the stack it walks reads its own levels.
    case "$series" in
      catchup/live/* | service/*) source=quic ;;
    esac
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
