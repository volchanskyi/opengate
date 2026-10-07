#!/usr/bin/env bash
# Tests for scripts/perf-vm-push.sh, which pushes each leg's bundle rows into the trend.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PUSH="$REPO_ROOT/scripts/perf-vm-push.sh"

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
mkdir -p "$WORK/bin"
cat >"$WORK/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$KUBECTL_ARGS"
cat >>"$KUBECTL_STDIN"
STUB
chmod +x "$WORK/bin/kubectl"

# bundle DIR PROFILE VERDICT P95 P50 CONNECT ERRORS — a leg's evidence, in the
# shape the harness writes it.
bundle() {
  local dir="$1" profile="$2" verdict="$3" p95="$4" p50="$5" connect="$6" errors="$7"
  mkdir -p "$dir"
  jq -n --arg profile "$profile" --arg verdict "$verdict" \
    --argjson p95 "$p95" --argjson p50 "$p50" --argjson connect "$connect" --argjson errors "$errors" '{
      run: {commit: "abc123", profile_name: $profile, profile_version: 1, environment: "runner",
            started_at: "2026-09-29T13:44:35Z", finished_at: "2026-09-29T13:51:35Z"},
      verdict: {result: $verdict},
      observations: [
        {series: "aggregate_error_rate", value: $errors},
        {series: "connect_p95_ms", value: $connect},
        {series: "register_p95_ms", value: $p95},
        {series: "register_p50_ms", value: $p50}
      ]}' >"$dir/bundle.json"
}

bundle "$WORK/perf-bundles/scaling-0.25" scaling valid 10000 595 188 0
bundle "$WORK/perf-bundles/volume-500" volume-500 valid 4.99 3.1 2 0
bundle "$WORK/perf-bundles/spike" spike invalid 9650 6140 45 0.2
bundle "$WORK/soak-bundle/soak" soak valid 9.11 3.25 2 0

run_push() {
  : >"$WORK/args"
  : >"$WORK/pushed"
  PATH="$WORK/bin:$PATH" KUBECTL_ARGS="$WORK/args" KUBECTL_STDIN="$WORK/pushed" \
    VM_RUN_STARTED_AT=1790689000 GITHUB_WORKFLOW="Performance Stack" GITHUB_SHA=abc123 GITHUB_RUN_ID=77 \
    "$PUSH" "$@"
}

echo "perf-vm-push:"

if out="$(run_push "$WORK/perf-bundles/scaling-0.25/bundle.json" "$WORK/perf-bundles/volume-500/bundle.json" \
  "$WORK/perf-bundles/spike/bundle.json" "$WORK/soak-bundle/soak/bundle.json" 2>&1)"; then
  pass "a night's legs are pushed"
else
  fail "a night's legs are pushed (out=[$out])"
fi

pushed="$(cat "$WORK/pushed")"
check() { # description, line
  if grep -qxF -- "$2" <<<"$pushed"; then pass "$1"; else fail "$1 (missing [$2] in [$pushed])"; fi
}
check "a leg's registration tail is named by the leg and the workload it measured" \
  'perf_latency_p95_ms{env="ci",leg="scaling-0.25",phase="register",workload="scaling/1"} 10000 1790689000000'
check "and its middle case beside it" \
  'perf_latency_p50_ms{env="ci",leg="volume-500",phase="register",workload="volume-500/1"} 3.1 1790689000000'
check "and the connect tail" \
  'perf_latency_p95_ms{env="ci",leg="volume-500",phase="connect",workload="volume-500/1"} 2 1790689000000'
check "and the error rate" \
  'perf_error_rate{env="ci",leg="scaling-0.25",phase="aggregate",workload="scaling/1"} 0 1790689000000'
check "the endurance run is a leg of its own" \
  'perf_latency_p95_ms{env="ci",leg="soak",phase="register",workload="soak/1"} 9.11 1790689000000'

if grep -qF 'leg="spike"' <<<"$pushed"; then
  fail "a leg that did not measure the system stays out of the trend"
else
  pass "a leg that did not measure the system stays out of the trend"
fi
if grep -qF 'spike' <<<"$out" && grep -qi 'did not measure' <<<"$out"; then
  pass "and the push says which leg it left out"
else
  fail "and the push says which leg it left out (out=[$out])"
fi
samples="$(grep -vE '^ci_run_info' <<<"$pushed" || true)"
if grep -qE '[{,](commit|run_id)=' <<<"$samples"; then
  fail "a leg's sample names the measurement only"
else
  pass "a leg's sample names the measurement only"
fi

if out="$(run_push "$WORK/perf-bundles/spike/bundle.json" 2>&1)"; then
  fail "a night with nothing measured is refused (out=[$out])"
else
  pass "a night with nothing measured is refused"
fi

if run_push "$WORK/no-such/bundle.json" >/dev/null 2>&1; then
  fail "a missing bundle is refused"
else
  pass "a missing bundle is refused"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
