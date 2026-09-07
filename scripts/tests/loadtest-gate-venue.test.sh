#!/usr/bin/env bash
# A limit names a measurement the place it runs can actually produce.
#
# Twelve of the sixteen limits across the profiles named a browser-side
# measurement. The process that would have judged them runs inside the
# machine-side harness, which holds phases and machines and no browser-side row
# at all — so from in there they were unreachable by construction. Worse, the
# two profiles that run on a throwaway machine each carried a limit on a
# browser-side measurement, and no browser-side generator runs there at all: the
# measurement those limits name cannot exist in the venue those profiles run in,
# whoever is doing the judging.
#
# That is the same false green as a limit on a measurement the extraction never
# emits, one level up: not "this number never arrives" but "this whole kind of
# number never arrives here".
#
# So: for every profile a workflow names, the sources its limits mention must be
# sources that workflow actually runs. The check reads both sides — which
# profile each workflow names, and which generators it starts — so a workflow
# that gains a browser-side leg makes those limits legal without this file being
# edited.
#
# Run: ./scripts/tests/loadtest-gate-venue.test.sh
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

# workflows_naming — every workflow file that names this profile, whether by
# handing it to the harness or by naming it for the step that reads the limits.
workflows_naming() {
  grep -rlE "load/profiles/$1\.yaml" "$WORKFLOW_DIR" 2>/dev/null || true
}

# sources_run_by — which kinds of generator a workflow starts.
#
# "k6" is the browser side: the workflow runs a scenario through the k6 runner.
# "quic" is the machine side: it starts the load harness binary. A workflow that
# starts neither produces no rows at all, which is its own finding.
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

  # A profile no workflow names has no venue to be judged against. Giving every
  # profile a venue or deleting it is its own work; this file is about the ones
  # that have one.
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

# A sweep that reached no profile is a sweep that proves nothing, which is the
# same shape as the limits it exists to catch.
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
