#!/usr/bin/env bash
# Holds scripts/nightly-review.sh to what fetch lists as due and what done refuses.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
REVIEW="$REPO_ROOT/scripts/nightly-review.sh"

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

echo "nightly review:"

# Two daily workflows: one ran two hours ago, the other last ran three days ago.
REPO="$WORK/repo"
mkdir -p "$REPO/.github/workflows" "$WORK/bin"
git -C "$REPO" init --quiet
printf 'name: Arrived\non:\n  schedule:\n    - cron: "0 2 * * *"\njobs: {}\n' \
  >"$REPO/.github/workflows/arrived.yml"
printf 'name: Never came\non:\n  schedule:\n    - cron: "0 3 * * *"\njobs: {}\n' \
  >"$REPO/.github/workflows/never-came.yml"
recent="$(date -u -d '-2 hours' +%Y-%m-%dT%H:%M:%SZ)"
old="$(date -u -d '-3 days' +%Y-%m-%dT%H:%M:%SZ)"
printf '[%s,%s]\n' \
  "$(printf '{"databaseId":200,"workflowName":"Arrived","status":"completed","conclusion":"success","createdAt":"%s","startedAt":"%s"}' "$recent" "$recent")" \
  "$(printf '{"databaseId":100,"workflowName":"Never came","status":"completed","conclusion":"success","createdAt":"%s","startedAt":"%s"}' "$old" "$old")" \
  >"$WORK/runs.json"
printf '#!/usr/bin/env bash\ncat %q\n' "$WORK/runs.json" >"$WORK/bin/gh"
chmod +x "$WORK/bin/gh"

review() { # subcommand and arguments; sets RC
  RC=0
  (cd "$REPO" && PATH="$WORK/bin:$PATH" "$REVIEW" "$@") >"$WORK/out" 2>&1 || RC=$?
}
due_list() { cat "$REPO/.claude/.markers/nightly.due" 2>/dev/null || true; }

review fetch
due="$(due_list)"
if grep -qE '^never-came\.yml missing' <<<"$due" && ! grep -qE '^arrived\.yml missing' <<<"$due"; then
  pass "a timed workflow with no run inside its window is listed as due"
else
  fail "a timed workflow with no run inside its window is listed as due (due=[$due])"
fi

writeup="$WORK/write-up.md"
printf '200 did-its-job: the trend reading and the report are both on the run\n' >"$writeup"
review "done" "$writeup"
if [ "$RC" -ne 0 ] && [ "$(due_list)" = "$due" ]; then
  pass "done refuses a write-up missing one due item"
else
  fail "done refuses a write-up missing one due item (rc=$RC, due=[$(due_list)])"
fi

printf '200 did-its-job: the trend reading is on the run\nnever-came.yml fine: GitHub dropped it\n' >"$writeup"
review "done" "$writeup"
if [ "$RC" -ne 0 ] && [ "$(due_list)" = "$due" ]; then
  pass "done refuses a line whose outcome is none of the four"
else
  fail "done refuses a line whose outcome is none of the four (rc=$RC, due=[$(due_list)])"
fi

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
