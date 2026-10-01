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
# omitted (floor-only for it), and the previous night is taken by date.

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
# by inspecting the requested match[] selector. Each series holds nights on its
# own dates: two nights back, last night, and — for rust — tonight's own reading,
# left by a re-run of this very night. The baseline is last night's, which is
# neither the highest reading nor the newest one in the store. Every invocation
# appends its args for later inspection.
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

# Tonight is the 29th.
TONIGHT="$(date -u -d '2026-09-29 09:08' +%s)"
STORE_MIDNIGHT="$(date -u -d '2026-09-29 00:00' +%s)"

# Run the real fetch script with the mock kubectl on PATH and the private-VM
# transport env. Per-case knobs (VM_FETCH_FIXTURE / KUBECTL_STATUS) are
# inherited from the caller.
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
  pass "assembles a one-line canonical row from last night's reading per language"
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

# --- Tonight is kept out by its date, not by its commit -----------------------
#
# The baseline was the last reading of a different commit, so a week without a
# merge compared each night against the week before it. It is last night's now,
# whatever code ran it, and tonight's own reading — a re-run's — is kept out
# because it carries tonight's date.
run_fetch >/dev/null
if grep -qF 'mutation_score{language="rust",env="ci"}' "$TMP_ROOT/kubectl.args" \
  && ! grep -qF 'commit' "$TMP_ROOT/kubectl.args"; then
  pass "reads the previous night by date and asks nothing about commits"
else
  fail "the baseline read must ask for the language's nights and nothing about commits (args=[$(cat "$TMP_ROOT/kubectl.args")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
