#!/usr/bin/env bash
# Offline tests for scripts/mutation-baseline-fetch.sh, using a mock kubectl on PATH.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FETCH="$REPO_ROOT/scripts/mutation-baseline-fetch.sh"

PASS=0
FAIL=0
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  printf '  FAIL %s\n' "$1" >&2
}

bin_dir="$TMP_ROOT/bin"
mkdir -p "$bin_dir"
cat >"$bin_dir/kubectl" <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"$KUBECTL_ARGS"
args="$*"
day() { printf '%s' "$(((STORE_MIDNIGHT + $1 * 86400 + 32400) * 1000))"; }
# emit LANG "VALUE@DAY ..." → one export-format series object
emit() {
  local lang="$1" values="" stamps="" point
  for point in $2; do
    values="${values:+$values,}${point%@*}"
    stamps="${stamps:+$stamps,}$(day "${point#*@}")"
  done
  printf '{"metric":{"__name__":"mutation_score","env":"ci","language":"%s"},"values":[%s],"timestamps":[%s]}\n' \
    "$lang" "$values" "$stamps"
}
case "${VM_FETCH_FIXTURE:-full}" in
  full)
    if grep -q 'language="rust"' <<<"$args"; then
      emit rust "92.0@-2 91.5@-1 99.0@0"
    elif grep -q 'language="go"' <<<"$args"; then
      emit go "88.25@-1"
    elif grep -q 'language="web"' <<<"$args"; then
      emit web "85.75@-2 84.5@-1"
    fi
    ;;
  partial)
    # Only rust has any prior night; go/web return nothing.
    if grep -q 'language="rust"' <<<"$args"; then
      emit rust "90.0@-1"
    fi
    ;;
  empty) ;;
esac
exit "${KUBECTL_STATUS:-0}"
EOF
chmod +x "$bin_dir/kubectl"

TONIGHT="$(date -u -d '2026-09-29 09:08' +%s)"
STORE_MIDNIGHT="$(date -u -d '2026-09-29 00:00' +%s)"

run_fetch() {
  : >"$TMP_ROOT/kubectl.args"
  (
    export PATH="$bin_dir:$PATH"
    export KUBECTL_ARGS="$TMP_ROOT/kubectl.args"
    export VM_NAMESPACE="observability"
    export VM_SERVICE="private-vm"
    export VM_RUN_STARTED_AT="$TONIGHT"
    export STORE_MIDNIGHT
    "$FETCH"
  )
}

# jq 1.7+ keeps a number's original text, so each number goes through `+ 0` to compare by value.
json_eq() {
  local norm='walk(if type == "number" then . + 0 else . end)'
  [ "$(jq -S "$norm" <<<"$1" 2>/dev/null)" = "$(jq -S "$norm" <<<"$2" 2>/dev/null)" ]
}

echo "mutation-baseline-fetch:"

if [ ! -x "$FETCH" ]; then
  fail "scripts/mutation-baseline-fetch.sh must exist and be executable"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

row="$(run_fetch)"
want='{"scores":{"rust":{"score_pct":91.5},"go":{"score_pct":88.25},"web":{"score_pct":84.5}}}'
if json_eq "$row" "$want" \
  && [ "$(printf '%s\n' "$row" | grep -c .)" = "1" ]; then
  pass "assembles a one-line canonical row from last night's reading per language"
else
  fail "full row should be $want (got: $row)"
fi

row="$(VM_FETCH_FIXTURE=partial run_fetch)"
want='{"scores":{"rust":{"score_pct":90}}}'
if json_eq "$row" "$want"; then
  pass "omits a language with no VM history (summarizer falls back to floor-only for it)"
else
  fail "partial row should carry only rust (got: $row)"
fi

code=0
row="$(VM_FETCH_FIXTURE=empty run_fetch)" || code=$?
if [ "$code" = "0" ] && [ -z "$row" ]; then
  pass "empty VM history is fail-open (no row, exit 0 ⇒ floor-only)"
else
  fail "empty history must print nothing and exit 0 (code=$code, row=$row)"
fi

code=0
row="$(KUBECTL_STATUS=19 run_fetch 2>/dev/null)" || code=$?
if [ "$code" = "0" ] && [ -z "$row" ]; then
  pass "transport failure is fail-open (no row, exit 0 ⇒ floor-only)"
else
  fail "transport failure must print nothing and exit 0 (code=$code, row=$row)"
fi

run_fetch >/dev/null
if grep -qF 'mutation_score{language="rust",env="ci"}' "$TMP_ROOT/kubectl.args" \
  && ! grep -qF 'commit' "$TMP_ROOT/kubectl.args"; then
  pass "reads the previous night by date and asks nothing about commits"
else
  fail "the baseline read must ask for the language's nights and nothing about commits (args=[$(cat "$TMP_ROOT/kubectl.args")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
