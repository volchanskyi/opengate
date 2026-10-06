#!/usr/bin/env bash
# Tests scripts/depcruise-check.sh: any error fails, warnings fail past the snapshot or at all
# once flipped, and a report it cannot read is a missing prerequisite.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$ROOT/scripts/depcruise-check.sh"

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
REPO="$WORK/repo"
mkdir -p "$WORK/bin" "$REPO/scripts" "$REPO/web" "$REPO/.claude/.markers/arch-lint-flipped"
ln -s "$CHECK" "$REPO/scripts/depcruise-check.sh"

cat >"$WORK/bin/npx" <<'NPX'
#!/usr/bin/env bash
printf '%s\n' "$PWD" >"$FAKE_STATE"
cat "$FAKE_REPORT"
[ "$(jq -r '.summary.error' "$FAKE_REPORT" 2>/dev/null)" = "0" ]
NPX
chmod +x "$WORK/bin/npx"

report() { # warn count, then error violations as rule:from pairs
  local warn="$1" errors="[]" pair
  shift
  for pair in "$@"; do
    errors="$(jq -c --arg r "${pair%%:*}" --arg f "${pair#*:}" \
      '. + [{rule: {name: $r, severity: "error"}, from: $f, to: "node_modules/vitest/dist/index.d.ts"}]' \
      <<<"$errors")"
  done
  jq -n --argjson w "$warn" --argjson v "$errors" \
    '{summary: {warn: $w, error: ($v | length), violations: $v}}' >"$WORK/report.json"
}

RC=0
run_check() { # snapshot warn, flipped (yes/no)
  printf '{"warn": %s}\n' "$1" >"$REPO/web/dependency-cruiser.snapshot.json"
  rm -f "$REPO/.claude/.markers/arch-lint-flipped/depcruise"
  if [ "$2" = yes ]; then
    : >"$REPO/.claude/.markers/arch-lint-flipped/depcruise"
  fi
  RC=0
  env PATH="$WORK/bin:$PATH" FAKE_REPORT="$WORK/report.json" FAKE_STATE="$WORK/cwd" \
    bash "$REPO/scripts/depcruise-check.sh" >"$WORK/out" 2>&1 || RC=$?
}
out() { cat "$WORK/out"; }

echo "depcruise-check:"

report 0
run_check 0 no
if [ "$RC" -eq 0 ]; then
  pass "a clean report passes"
else
  fail "a clean report passes (rc=$RC out=[$(out)])"
fi
if grep -qxF "$REPO/web" "$WORK/cwd"; then
  pass "and depcruise runs in web/"
else
  fail "and depcruise runs in web/ (ran in [$(cat "$WORK/cwd")])"
fi

report 0 not-to-dev-dep:src/features/devices/DeviceDetail.testkit.tsx
run_check 0 no
if [ "$RC" -eq 1 ] && grep -qF 'not-to-dev-dep' <<<"$(out)" \
  && grep -qF 'src/features/devices/DeviceDetail.testkit.tsx' <<<"$(out)"; then
  pass "an error fails, naming its rule and the importing file"
else
  fail "an error fails, naming its rule and the importing file (rc=$RC out=[$(out)])"
fi

report 0 not-to-dev-dep:src/a.ts
run_check 3 no
if [ "$RC" -eq 1 ]; then
  pass "an error fails whatever the warning baseline allows"
else
  fail "an error fails whatever the warning baseline allows (rc=$RC out=[$(out)])"
fi

report 2
run_check 2 no
if [ "$RC" -eq 0 ]; then
  pass "warnings up to the snapshot pass before the flip"
else
  fail "warnings up to the snapshot pass before the flip (rc=$RC out=[$(out)])"
fi

report 3
run_check 2 no
if [ "$RC" -eq 1 ] && grep -qF 'current=3 baseline=2' <<<"$(out)"; then
  pass "warnings past the snapshot fail, naming both counts"
else
  fail "warnings past the snapshot fail, naming both counts (rc=$RC out=[$(out)])"
fi

report 1
run_check 5 yes
if [ "$RC" -eq 1 ]; then
  pass "a flipped gate fails on any warning"
else
  fail "a flipped gate fails on any warning (rc=$RC out=[$(out)])"
fi

echo 'Error: config not found' >"$WORK/report.json"
run_check 0 yes
if [ "$RC" -eq 2 ]; then
  pass "a report that is not JSON is a missing prerequisite"
else
  fail "a report that is not JSON is a missing prerequisite (rc=$RC out=[$(out)])"
fi

if grep -qF 'scripts/depcruise-check.sh' "$ROOT/scripts/precommit-gauntlet.sh" \
  && ! grep -qF 'depcruise src' "$ROOT/scripts/precommit-gauntlet.sh"; then
  pass "the gauntlet runs the script, and no depcruise of its own"
else
  fail "the gauntlet runs the script, and no depcruise of its own"
fi
web_lint="$(awk '/^  web-lint:/ { on = 1; next } on && /^  [A-Za-z0-9_-]+:[[:space:]]*$/ { on = 0 } on' \
  "$ROOT/.github/workflows/ci.yml")"
if [ -z "$web_lint" ]; then
  fail "CI's web-lint job was found"
elif grep -qF 'scripts/depcruise-check.sh' <<<"$web_lint" && ! grep -qF 'depcruise src' <<<"$web_lint"; then
  pass "CI's web-lint job runs the script, and no depcruise of its own"
else
  fail "CI's web-lint job runs the script, and no depcruise of its own"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
