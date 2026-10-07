#!/usr/bin/env bash
# Under pipefail a writer piped into `grep -q` loses a match that is present, and a here-string
# keeps it; the sweep holds every pipefail script to the here-string form.

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

# The haystack far exceeds the pipe capacity with the needle on its first line.
# Each shape runs in its own bash -c because the haystack is too large for argv.
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

# The sweep covers scripts that enable pipefail and counts them so an empty read fails.
# Command substitutions are removed depth-aware first; any writer into an early-exit reader counts.
strip_substitutions() {
  awk '{
    out = ""; depth = 0; n = length($0)
    for (i = 1; i <= n; i++) {
      c = substr($0, i, 1)
      if (depth == 0 && c == "$" && substr($0, i + 1, 1) == "(") { depth = 1; i++; continue }
      if (depth > 0) {
        if (c == "(") depth++
        else if (c == ")") depth--
        continue
      }
      out = out c
    }
    print out
  }'
}

# A single `|` feeding a reader that can stop before its input ends; `||` is a branch.
EARLY_EXIT_READER='(^|[^|])\|[[:space:]]*(grep([[:space:]]+-[a-zA-Z]+)*[[:space:]]+-[a-zA-Z]*[qlmL][a-zA-Z]*|head)([[:space:]]|$)'

scanned=0
offenders=""
while IFS= read -r file; do
  [ "$file" = "$SELF" ] && continue
  grep -q 'pipefail' "$ROOT/$file" || continue
  scanned=$((scanned + 1))
  # Continuation lines are joined so a pipe split across lines is matched whole.
  joined="$(awk '
    { line = line $0; nr = nr ? nr : NR }
    /(\||\\)[[:space:]]*$/ { sub(/\\[[:space:]]*$/, "", line); next }
    { print nr ":" line; line = ""; nr = 0 }
    END { if (line != "") print nr ":" line }
  ' "$ROOT/$file" | grep -vE '^[0-9]+:[[:space:]]*#' | strip_substitutions)"
  while IFS= read -r hit; do
    [ -n "$hit" ] || continue
    offenders="$offenders  $file:$hit"$'\n'
  done < <(grep -E "$EARLY_EXIT_READER" <<<"$joined" || true)
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
