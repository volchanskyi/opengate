#!/usr/bin/env bash
# Offline tests for scripts/mutation-baseline-fetch.sh — the thin adapter that
# reconstructs the previous per-language mutation baseline from VictoriaMetrics
# so scripts/mutation-summarize.sh's drop-rule (score fell >2pp from the
# previous run) can fire in CI. Without a restored baseline previous_row is
# always null and only the absolute floor ever trips.
#
# Mocks kubectl on PATH (the scripts/tests/vm-query.test.sh pattern) so the
# shared read-back lib scripts/lib/vm-query.sh talks to canned /api/v1/export
# fixtures. Asserts: newest-per-language row assembly, fail-open (empty /
# transport failure ⇒ empty stdout, exit 0), a language missing in VM is simply
# omitted (floor-only for it), and the current commit is excluded from the query.

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

# Mock kubectl serves the /api/v1/export API with a per-language fixture chosen
# by inspecting the requested match[] selector. Two samples are emitted for some
# languages (older + newer timestamp) to prove the adapter takes the NEWEST, not
# the highest, sample. Every invocation appends its args for later inspection.
bin_dir="$TMP_ROOT/bin"
mkdir -p "$bin_dir"
cat >"$bin_dir/kubectl" <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"$KUBECTL_ARGS"
args="$*"
# emit LANG COMMIT VALUE TS  → one export-format series object (single point)
emit() {
  printf '{"metric":{"__name__":"mutation_score","commit":"%s","env":"ci","language":"%s"},"values":[%s],"timestamps":[%s]}\n' \
    "$2" "$1" "$3" "$4"
}
case "${VM_FETCH_FIXTURE:-full}" in
  full)
    if printf '%s' "$args" | grep -q 'language="rust"'; then
      emit rust older 92.0 1000
      emit rust newer 91.5 2000
    elif printf '%s' "$args" | grep -q 'language="go"'; then
      emit go newer 88.25 2000
    elif printf '%s' "$args" | grep -q 'language="web"'; then
      emit web older 85.75 1000
      emit web newer 84.5 2000
    fi
    ;;
  partial)
    # Only rust has any prior sample; go/web return nothing.
    if printf '%s' "$args" | grep -q 'language="rust"'; then
      emit rust newer 90.0 2000
    fi
    ;;
  empty) ;;
esac
exit "${KUBECTL_STATUS:-0}"
EOF
chmod +x "$bin_dir/kubectl"

# Run the real fetch script with the mock kubectl on PATH and the private-VM
# transport env. Per-case knobs (VM_FETCH_FIXTURE / KUBECTL_STATUS /
# VM_EXCLUDE_COMMIT) are inherited from the caller.
run_fetch() {
  : >"$TMP_ROOT/kubectl.args"
  (
    export PATH="$bin_dir:$PATH"
    export KUBECTL_ARGS="$TMP_ROOT/kubectl.args"
    export VM_NAMESPACE="observability"
    export VM_SERVICE="private-vm"
    "$FETCH"
  )
}

# json_eq A B → 0 when A and B are the same JSON value (key order ignored, and
# numbers compared by value not literal). jq 1.7+ preserves a number's original
# text, so 90.0 and 90 render differently under a bare `jq -S .`; forcing each
# number through `+ 0` canonicalizes both so the comparison stays version-stable.
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

# --- Full row: newest sample per language, one canonical line -----------------
row="$(run_fetch)"
want='{"scores":{"rust":{"score_pct":91.5},"go":{"score_pct":88.25},"web":{"score_pct":84.5}}}'
if json_eq "$row" "$want" \
  && [ "$(printf '%s\n' "$row" | grep -c .)" = "1" ]; then
  pass "assembles a one-line canonical row from the newest per-language VM sample"
else
  fail "full row should be $want (got: $row)"
fi

# --- A language missing in VM is omitted (floor-only applies to it) -----------
row="$(VM_FETCH_FIXTURE=partial run_fetch)"
want='{"scores":{"rust":{"score_pct":90}}}'
if json_eq "$row" "$want"; then
  pass "omits a language with no VM history (summarizer falls back to floor-only for it)"
else
  fail "partial row should carry only rust (got: $row)"
fi

# --- Fail-open: no history at all ⇒ empty stdout, exit 0 ----------------------
code=0
row="$(VM_FETCH_FIXTURE=empty run_fetch)" || code=$?
if [ "$code" = "0" ] && [ -z "$row" ]; then
  pass "empty VM history is fail-open (no row, exit 0 ⇒ floor-only)"
else
  fail "empty history must print nothing and exit 0 (code=$code, row=$row)"
fi

# --- Fail-open: transport failure ⇒ empty stdout, exit 0 ---------------------
code=0
row="$(KUBECTL_STATUS=19 run_fetch 2>/dev/null)" || code=$?
if [ "$code" = "0" ] && [ -z "$row" ]; then
  pass "transport failure is fail-open (no row, exit 0 ⇒ floor-only)"
else
  fail "transport failure must print nothing and exit 0 (code=$code, row=$row)"
fi

# --- Current commit excluded from the baseline query -------------------------
VM_EXCLUDE_COMMIT="deadbeef" run_fetch >/dev/null
if grep -qF 'commit!="deadbeef"' "$TMP_ROOT/kubectl.args" \
  && grep -qF 'mutation_score{language="rust",env="ci",commit!="deadbeef"}' "$TMP_ROOT/kubectl.args"; then
  pass "excludes the current commit so a re-run never compares against itself"
else
  fail "query must exclude the current commit when VM_EXCLUDE_COMMIT is set"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
