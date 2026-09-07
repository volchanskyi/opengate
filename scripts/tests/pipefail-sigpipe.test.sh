#!/usr/bin/env bash
# An assertion survives its own pipe.
#
# `printf '%s\n' "$haystack" | grep -q "$needle"` reads as "does the haystack
# contain the needle". Under `set -o pipefail` it answers a different question.
# `grep -q` exits the moment it matches, so when the haystack is larger than the
# pipe will hold, printf's remaining write fails with EPIPE and returns non-zero;
# pipefail promotes that to the pipeline's status, and the caller reads a match
# that happened as no match at all.
#
# It fails in both directions, and the second is the one that matters:
#
#   contains → reports the needle missing when it is present  (a flake)
#   lacks    → reports the needle absent when it is present   (a false green)
#
# The second shape is the one ci-cd-determinism.md exists to refuse: the check
# ran, the forbidden thing was there, and the gate went green.
#
# It hides well. Nothing fires below the pipe's capacity, so every small fixture
# passes forever; it needs a match early in a multi-line haystack with enough
# left to write afterwards. That is an ordinary shape — a needle near the top of
# a workflow file — and where the boundary sits depends on the machine, so a
# workstation stays green while CI fails one run in ten. A nightly drill's test
# lost three assertions this way on the second attempt of a commit whose first
# attempt had passed all three.
#
# The fix is to stop making it a pipeline: `grep -q "$needle" <<<"$haystack"`
# matches identically — a here-string appends the same trailing newline printf
# did — and its status is grep's alone.
#
# Run: ./scripts/tests/pipefail-sigpipe.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SELF="scripts/tests/pipefail-sigpipe.test.sh"

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

echo "pipefail-sigpipe:"

# --- the defect is real, and the replacement closes it ------------------------
#
# A haystack far past any pipe's capacity with the needle on its first line, so
# grep matches and exits with almost everything still unwritten. Sized to make
# the race certain rather than occasional: the point here is to demonstrate the
# shape, not to find the boundary.
# Each shape runs in its own bash -c, which builds the haystack itself: it is
# far too large to pass through argv, and running it out of process keeps the
# banned form's ERR from escaping into this script while leaving `set -o
# pipefail` genuinely in force for it.
run_shape() {
  bash -c '
    set -euo pipefail
    hay="$(
      printf "NEEDLE\n"
      seq 1 40000 | sed "s/$/ padding that keeps the writer writing/"
    )"
    case "$1" in
      piped) if printf "%s\n" "$hay" | grep -qF -- "NEEDLE"; then echo found; else echo missed; fi ;;
      herestring) if grep -qF -- "NEEDLE" <<<"$hay"; then echo found; else echo missed; fi ;;
    esac
  ' _ "$1" 2>/dev/null
}

if [ "$(run_shape piped)" = "missed" ]; then
  pass "a needle that is present is lost when the assertion goes through a pipe"
else
  fail "the banned shape did not lose the match — this guard is no longer demonstrating anything"
fi

if [ "$(run_shape herestring)" = "found" ]; then
  pass "and is found when the same assertion does not"
else
  fail "the here-string form lost a needle that is present"
fi

# The false green. `assert_lacks` is written as "if it matches, fail" — so the
# lost match becomes a pass, and a forbidden string sails through the gate.
if [ "$(run_shape piped)" = "missed" ]; then
  pass "a forbidden needle that is present reports clean through a pipe"
else
  fail "the false-green direction no longer reproduces"
fi

if [ "$(run_shape herestring)" = "found" ]; then
  pass "and is caught when the assertion does not go through one"
else
  fail "the here-string form missed a forbidden needle that is present"
fi

# --- nothing in the repository is written that way ----------------------------
#
# Scoped to scripts that enable pipefail, since that is what promotes the write
# error into the pipeline's verdict. The count is kept so a sweep that reached
# nothing — a moved tree, a broken glob — fails rather than passing silently.
scanned=0
offenders=""
while IFS= read -r file; do
  [ "$file" = "$SELF" ] && continue
  grep -q 'pipefail' "$ROOT/$file" || continue
  scanned=$((scanned + 1))
  # A pipeline is often written across lines, with the pipe or a backslash left
  # at the end of one and `grep -q` starting the next. Joining continuations
  # before matching is what makes the sweep see those; the commit and push
  # guards are both written that way.
  while IFS= read -r hit; do
    offenders="$offenders  $file:$hit"$'\n'
  done < <(awk '
    { line = line $0; nr = nr ? nr : NR }
    /(\||\\)[[:space:]]*$/ { sub(/\\[[:space:]]*$/, "", line); next }
    { print nr ":" line; line = ""; nr = 0 }
    END { if (line != "") print nr ":" line }
  ' "$ROOT/$file" | grep -E '(printf|echo)[^|]*\| *grep -q' || true)
done < <(git -C "$ROOT" ls-files -- 'scripts/*.sh' 'scripts/**/*.sh' 'deploy/**/*.sh' '.claude/**/*.sh')

if [ "$scanned" -eq 0 ]; then
  fail "the sweep read no pipefail script at all — it is checking nothing"
else
  pass "the sweep read $scanned scripts that enable pipefail"
fi

if [ -n "$offenders" ]; then
  fail "an assertion is piped into grep -q under pipefail"
  printf '%s' "$offenders" >&2
  printf '    feed grep a here-string instead of a pipe: grep -q -- NEEDLE %s HAYSTACK\n' '<<<' >&2
else
  pass "no assertion is piped into grep -q under pipefail"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done >&2
  exit 1
fi
