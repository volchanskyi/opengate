#!/usr/bin/env bash
# Tests for the mutation workflow: timeout/exit-code classification, the Go
# source partition, shard-report validation/merge, run-status generation, and
# summarizer error propagation.
#
# Bug history: GitHub Actions run 27743482464 cancelled the Go gremlins leg at
# the job cap before server/mutation-report.json could be uploaded; the publish
# job then collapsed mutation-summarize.sh exit 2 (missing input) into
# "regression=1", mislabeling an incomplete run as a score regression. Exit-code
# semantics are pinned here: 0 = clean, 1 = score regression, 2 =
# incomplete/malformed input.
#
# Scaling: the Go leg uses directory/file mutation units so every non-test Go
# source under server/ is mutated exactly once (or globally excluded). The shard
# split lives in one place (scripts/lib/mutation-shards.sh); these tests assert
# the workflow matrix matches it and prevent cross-shard duplicate counting.
#
# Run: ./scripts/tests/mutation-workflow.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOW="$REPO_ROOT/.github/workflows/mutation.yml"
SHARDS_LIB="$REPO_ROOT/scripts/lib/mutation-shards.sh"
MERGE="$REPO_ROOT/scripts/mutation-merge-go.sh"
MERGE_RUST="$REPO_ROOT/scripts/mutation-merge-rust.sh"
STATUS_BUILD="$REPO_ROOT/scripts/mutation-status-build.sh"
STATUS_PUSH="$REPO_ROOT/scripts/mutation-status-vm-push.sh"
SUMMARIZE="$REPO_ROOT/scripts/mutation-summarize.sh"

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

echo "mutation-workflow:"

if [ ! -f "$WORKFLOW" ]; then
  echo "FAIL: $WORKFLOW not found" >&2
  exit 1
fi

# --- Static workflow contract -------------------------------------------------

# The job timeout is a flat 75 minutes. Every leg fits under it: the Go leg is
# package-sharded and the rust leg is split with `cargo mutants --shard`.
if grep -qE "^[[:space:]]*timeout-minutes:[[:space:]]*75[[:space:]]*$" "$WORKFLOW"; then
  pass "mutation job timeout is a flat 75 minutes (every sharded leg fits under it)"
else
  fail "mutation job must set timeout-minutes: 75"
fi

# Rust must use four exact cargo-mutants shards. Two legs no longer fit after the
# Edge Sentinel local-store/discovery growth.
rust_shard_legs="$(grep -cE '^[[:space:]]*-[[:space:]]*\{[[:space:]]*language:[[:space:]]*rust,[[:space:]]*shard:[[:space:]]*rust-[0-9]+' "$WORKFLOW")"
rust_shard_selectors="$(sed -nE 's/.*language:[[:space:]]*rust.*rust_shard:[[:space:]]*"([0-9]+\/[0-9]+)".*/\1/p' "$WORKFLOW" | sort | tr '\n' ' ')"
if [ "$rust_shard_legs" = "4" ] && [ "$rust_shard_selectors" = "0/4 1/4 2/4 3/4 " ]; then
  pass "rust mutation leg uses exactly four 0/4..3/4 shards"
else
  fail "rust matrix must contain exactly 0/4..3/4 (legs=$rust_shard_legs selectors='$rust_shard_selectors')"
fi

if grep -qE 'cargo mutants .*--shard[[:space:]]+.*matrix\.rust_shard' "$WORKFLOW"; then
  pass "rust step selects its shard via cargo mutants --shard (matrix.rust_shard)"
else
  fail "rust step must run cargo mutants with --shard from matrix.rust_shard"
fi

if grep -q 'SUMMARY_STATUS=' "$WORKFLOW" \
  && grep -q 'Mutation summary input missing or invalid' "$WORKFLOW" \
  && grep -qE '^[[:space:]]*2\)' "$WORKFLOW"; then
  pass "summarize exit 2 is classified as incomplete input"
else
  fail "summarize exit 2 must fail as incomplete input, not regression"
fi

if grep -qE '^[[:space:]]*0\)[[:space:]]*REGRESSION=0[[:space:]]*;;' "$WORKFLOW" \
  && grep -qE '^[[:space:]]*1\)[[:space:]]*REGRESSION=1[[:space:]]*;;' "$WORKFLOW"; then
  pass "summarize exit 0/1 preserve clean/regression semantics"
else
  fail "summarize exit 0/1 must preserve clean/regression semantics"
fi

# --- Shard partition (single source of truth) ---------------------------------

if [ -f "$SHARDS_LIB" ]; then
  # shellcheck source=/dev/null
  . "$SHARDS_LIB"
  pass "scripts/lib/mutation-shards.sh exists and sources cleanly"

  # Workflow matrix Go shard ids must match the shard map (no drift).
  want_ids="$(mutation_go_shards | tr ' ' '\n' | sort | tr '\n' ' ')"
  have_ids="$(grep -oE 'shard:[[:space:]]*go-[a-z0-9-]+' "$WORKFLOW" \
    | sed -E 's/shard:[[:space:]]*//' | sort -u | tr '\n' ' ')"
  if [ "$want_ids" = "$have_ids" ]; then
    pass "workflow matrix Go shard ids match the shard map"
  else
    fail "workflow shard ids drifted from map: map='$want_ids' wf='$have_ids'"
  fi

  expected_rust="$(mutation_rust_shards | tr ' ' '\n' | sort | tr '\n' ' ')"
  workflow_rust="$(grep -oE 'shard:[[:space:]]*rust-[0-9]+' "$WORKFLOW" \
    | sed -E 's/shard:[[:space:]]*//' | sort -u | tr '\n' ' ')"
  if [ "$expected_rust" = "$workflow_rust" ] \
    && [ "$(mutation_all_shards)" = "rust-1 rust-2 rust-3 rust-4 $(mutation_go_shards) web" ]; then
    pass "shard library exposes the exact Rust/Go/Web expected set"
  else
    fail "expected shard set drifted (rust map='$expected_rust' wf='$workflow_rust' all='$(mutation_all_shards)')"
  fi

  read -r -a go_shards <<<"$(mutation_go_shards)"
  declare -a all_units=()
  declare -A unit_owner=()
  declare -A shard_regex=()

  # Reverse-check unit declarations before using them for source coverage.
  unit_bad=""
  for shard in "${go_shards[@]}"; do
    shard_regex[$shard]="$(mutation_go_shard_exclude_regex "$shard")"
    read -r -a shard_units <<<"$(mutation_go_shard_units "$shard")"
    for unit in "${shard_units[@]}"; do
      if [ -n "${unit_owner[$unit]:-}" ]; then
        unit_bad="$unit_bad [$shard:$unit:also-${unit_owner[$unit]}]"
      fi
      unit_owner[$unit]="$shard"
      all_units+=("$unit")
      case "$unit" in
        dir:*)
          [ -d "$REPO_ROOT/server/${unit#dir:}" ] \
            || unit_bad="$unit_bad [$shard:$unit:no-such-dir]"
          ;;
        file:*)
          [ -f "$REPO_ROOT/server/${unit#file:}" ] \
            || unit_bad="$unit_bad [$shard:$unit:no-such-file]"
          ;;
        *) unit_bad="$unit_bad [$shard:$unit:bad-kind]" ;;
      esac
    done
  done
  if [ -z "$unit_bad" ]; then
    pass "all Go mutation units use valid dir:/file: paths"
  else
    fail "invalid Go mutation unit declarations:$unit_bad"
  fi

  # Every non-test Go source under server/ is either globally excluded or
  # belongs to exactly one mutation unit. This catches sources outside
  # internal/* (notably tests/loadtest) and duplicate directory/file overlap.
  partition_bad=""
  regex_bad=""
  while IFS= read -r source; do
    rel="${source#"$REPO_ROOT/server/"}"
    global=0
    if printf '%s\n' "$rel" | grep -qE "$(mutation_go_global_excludes)"; then
      global=1
    fi

    matches=0
    owner=""
    for unit in "${all_units[@]}"; do
      if mutation_go_unit_matches "$unit" "$rel"; then
        matches=$((matches + 1))
        owner="${unit_owner[$unit]}"
      fi
    done

    if [ "$global" -eq 0 ] && [ "$matches" -ne 1 ]; then
      partition_bad="$partition_bad [$rel:matches=$matches]"
      continue
    fi

    # The generated/entry-point/test-helper carve-outs must be excluded by all
    # shard regexes. A real source must be included only by its owner.
    for shard in "${go_shards[@]}"; do
      excl="${shard_regex[$shard]}"
      if [ "$global" -eq 1 ]; then
        printf '%s\n' "$rel" | grep -qE "$excl" \
          || regex_bad="$regex_bad [$shard:$rel:global-not-excluded]"
      elif [ "$shard" = "$owner" ]; then
        if printf '%s\n' "$rel" | grep -qE "$excl"; then
          regex_bad="$regex_bad [$shard:$rel:own-source-excluded]"
        fi
      else
        printf '%s\n' "$rel" | grep -qE "$excl" \
          || regex_bad="$regex_bad [$shard:$rel:other-source-not-excluded]"
      fi
    done
  done < <(find "$REPO_ROOT/server" -type f -name '*.go' ! -name '*_test.go' | sort)

  if [ -z "$partition_bad" ]; then
    pass "all non-test server Go sources are assigned once or globally excluded"
  else
    fail "whole-server Go source partition mismatch:$partition_bad"
  fi
  if [ -z "$regex_bad" ]; then
    pass "each Go shard regex includes only its own mutation units"
  else
    fail "Go shard exclude regex mismatch:$regex_bad"
  fi

  loadtest_bad=""
  for f in soak.go soak_backfill.go soak_telemetry.go; do
    owners=0
    owner=""
    for unit in "${all_units[@]}"; do
      if mutation_go_unit_matches "$unit" "tests/loadtest/$f"; then
        owners=$((owners + 1))
        owner="${unit_owner[$unit]}"
      fi
    done
    [ "$owners" -eq 1 ] && [ "$owner" = "go-pure-2" ] \
      || loadtest_bad="$loadtest_bad [$f:owners=$owners:$owner]"
  done
  if [ -z "$loadtest_bad" ] \
    && printf 'tests/loadtest/main.go\n' | grep -qE "$(mutation_go_global_excludes)"; then
    pass "loadtest helpers mutate once in go-pure-2 while main.go stays excluded"
  else
    fail "loadtest mutation ownership is wrong:$loadtest_bad"
  fi

  api_bad=""
  while IFS= read -r source; do
    rel="${source#"$REPO_ROOT/server/"}"
    [ "$rel" = "internal/api/openapi_gen.go" ] && continue
    owners=0
    for unit in "${all_units[@]}"; do
      mutation_go_unit_matches "$unit" "$rel" && owners=$((owners + 1))
    done
    [ "$owners" -eq 1 ] || api_bad="$api_bad [$rel:owners=$owners]"
  done < <(find "$REPO_ROOT/server/internal/api" -maxdepth 1 -type f -name '*.go' ! -name '*_test.go' | sort)
  for required in handlers_device_history.go handlers_purge.go; do
    found=0
    for unit in "${all_units[@]}"; do
      mutation_go_unit_matches "$unit" "internal/api/$required" && found=$((found + 1))
    done
    [ "$found" -eq 1 ] || api_bad="$api_bad [$required:owners=$found]"
  done
  if [ -z "$api_bad" ]; then
    pass "every API source, including history and purge, is assigned once"
  else
    fail "API file-unit partition mismatch:$api_bad"
  fi

  # Global excludes must stay in sync with server/.gremlins.yaml exclude-files.
  globals="$(mutation_go_global_excludes)"
  sync_bad=""
  while IFS= read -r pat; do
    pat="${pat//\\\\/\\}"
    case "$globals" in
      *"$pat"*) : ;;
      *) sync_bad="$sync_bad [$pat]" ;;
    esac
  done < <(sed -nE 's/^[[:space:]]*-[[:space:]]*"([^"]+)".*/\1/p' "$REPO_ROOT/server/.gremlins.yaml")
  if [ -z "$sync_bad" ]; then
    pass "global excludes mirror server/.gremlins.yaml exclude-files"
  else
    fail "global excludes out of sync with .gremlins.yaml:$sync_bad"
  fi
else
  fail "scripts/lib/mutation-shards.sh must exist (single source of shard split)"
fi

# --- Shard-report merge -------------------------------------------------------

if [ -x "$MERGE" ]; then
  tmp="$(mktemp -d)"
  printf '%s' '{"mutants_killed":10,"mutants_lived":2,"mutants_not_covered":3,"mutants_not_viable":1}' >"$tmp/r1.json"
  printf '%s' '{"mutants_killed":5,"mutants_lived":1,"mutants_not_covered":0,"mutants_not_viable":4}' >"$tmp/r2.json"
  if "$MERGE" "$tmp/out.json" "$tmp/r1.json" "$tmp/r2.json" >/dev/null 2>&1 \
    && [ "$(jq -r '.mutants_killed' "$tmp/out.json")" = "15" ] \
    && [ "$(jq -r '.mutants_lived' "$tmp/out.json")" = "3" ] \
    && [ "$(jq -r '.mutants_not_covered' "$tmp/out.json")" = "3" ] \
    && [ "$(jq -r '.mutants_not_viable' "$tmp/out.json")" = "5" ]; then
    pass "mutation-merge-go.sh sums shard report counts element-wise"
  else
    fail "mutation-merge-go.sh must sum shard report counts"
  fi
  # A missing shard report (a cancelled/failed shard) must FAIL the merge and
  # write no output, so publish reports an incomplete run rather than a silent
  # partial score from the surviving shards.
  rm -f "$tmp/out.json"
  if "$MERGE" "$tmp/out.json" "$tmp/r1.json" "$tmp/MISSING.json" >/dev/null 2>&1; then
    fail "mutation-merge-go.sh must fail when a shard report is missing"
  elif [ -f "$tmp/out.json" ]; then
    fail "mutation-merge-go.sh must not write a partial report when a shard is missing"
  else
    pass "mutation-merge-go.sh fails (no output) on a missing shard report"
  fi
  for fixture in malformed missing-field non-numeric; do
    case "$fixture" in
      malformed) printf '%s' '{bad json' >"$tmp/bad.json" ;;
      missing-field) printf '%s' '{"mutants_killed":1,"mutants_lived":0,"mutants_not_covered":0}' >"$tmp/bad.json" ;;
      non-numeric) printf '%s' '{"mutants_killed":"1","mutants_lived":0,"mutants_not_covered":0,"mutants_not_viable":0}' >"$tmp/bad.json" ;;
    esac
    printf '%s' 'stale' >"$tmp/out.json"
    if "$MERGE" "$tmp/out.json" "$tmp/r1.json" "$tmp/bad.json" >/dev/null 2>&1; then
      fail "mutation-merge-go.sh must reject $fixture input"
    elif [ -e "$tmp/out.json" ]; then
      fail "mutation-merge-go.sh must remove output after $fixture input"
    else
      pass "mutation-merge-go.sh rejects $fixture input atomically"
    fi
  done
  rm -rf "$tmp"
else
  fail "scripts/mutation-merge-go.sh must exist and be executable"
fi

# --- Rust shard-outcome merge -------------------------------------------------

if [ -x "$MERGE_RUST" ]; then
  tmp="$(mktemp -d)"
  printf '%s' '{"end_time":"2026-07-13T01:00:00Z","caught":10,"missed":2,"timeout":1,"unviable":3}' >"$tmp/r1.json"
  printf '%s' '{"end_time":"2026-07-13T01:01:00Z","caught":5,"missed":1,"timeout":0,"unviable":4}' >"$tmp/r2.json"
  if "$MERGE_RUST" "$tmp/out.json" "$tmp/r1.json" "$tmp/r2.json" >/dev/null 2>&1 \
    && [ "$(jq -r '.caught' "$tmp/out.json")" = "15" ] \
    && [ "$(jq -r '.missed' "$tmp/out.json")" = "3" ] \
    && [ "$(jq -r '.timeout' "$tmp/out.json")" = "1" ] \
    && [ "$(jq -r '.unviable' "$tmp/out.json")" = "7" ]; then
    pass "mutation-merge-rust.sh sums shard outcome counts element-wise"
  else
    fail "mutation-merge-rust.sh must sum shard outcome counts"
  fi
  # A missing shard outcome file (cancelled/failed shard) must FAIL the merge and
  # write no output, mirroring the Go merge: publish then reports an incomplete
  # run rather than a silent partial score from the surviving shard.
  rm -f "$tmp/out.json"
  if "$MERGE_RUST" "$tmp/out.json" "$tmp/r1.json" "$tmp/MISSING.json" >/dev/null 2>&1; then
    fail "mutation-merge-rust.sh must fail when a shard outcome file is missing"
  elif [ -f "$tmp/out.json" ]; then
    fail "mutation-merge-rust.sh must not write a partial report when a shard is missing"
  else
    pass "mutation-merge-rust.sh fails (no output) on a missing shard outcome file"
  fi
  for fixture in malformed null-end missing-field non-numeric; do
    case "$fixture" in
      malformed) printf '%s' '{bad json' >"$tmp/bad.json" ;;
      null-end) printf '%s' '{"end_time":null,"caught":1,"missed":0,"timeout":0,"unviable":0}' >"$tmp/bad.json" ;;
      missing-field) printf '%s' '{"end_time":"2026-07-13T01:00:00Z","caught":1,"missed":0,"timeout":0}' >"$tmp/bad.json" ;;
      non-numeric) printf '%s' '{"end_time":"2026-07-13T01:00:00Z","caught":"1","missed":0,"timeout":0,"unviable":0}' >"$tmp/bad.json" ;;
    esac
    printf '%s' 'stale' >"$tmp/out.json"
    if "$MERGE_RUST" "$tmp/out.json" "$tmp/r1.json" "$tmp/bad.json" >/dev/null 2>&1; then
      fail "mutation-merge-rust.sh must reject $fixture input"
    elif [ -e "$tmp/out.json" ]; then
      fail "mutation-merge-rust.sh must remove output after $fixture input"
    else
      pass "mutation-merge-rust.sh rejects $fixture input atomically"
    fi
  done
  rm -rf "$tmp"
else
  fail "scripts/mutation-merge-rust.sh must exist and be executable"
fi

# publish must merge the rust shards through that script.
if grep -q 'mutation-merge-rust\.sh' "$WORKFLOW"; then
  pass "publish merges the rust shards via mutation-merge-rust.sh"
else
  fail "publish must merge rust shards via mutation-merge-rust.sh"
fi

# --- Complete/incomplete run status ------------------------------------------

make_complete_artifacts() {
  local root="$1" shard path
  rm -rf "$root"
  for shard in $(mutation_rust_shards); do
    path="$root/mutation-$shard/agent/mutants.out"
    mkdir -p "$path"
    printf '%s' '{"end_time":"2026-07-13T01:00:00Z","caught":10,"missed":1,"timeout":0,"unviable":0}' >"$path/outcomes.json"
  done
  for shard in $(mutation_go_shards); do
    path="$root/mutation-$shard/server"
    mkdir -p "$path"
    printf '%s' '{"mutants_killed":10,"mutants_lived":1,"mutants_not_covered":0,"mutants_not_viable":0}' >"$path/mutation-report-$shard.json"
  done
  path="$root/mutation-web/web/reports/mutation"
  mkdir -p "$path"
  printf '%s' '{"files":{"a.ts":{"mutants":[{"status":"Killed"}]}}}' >"$path/mutation.json"
}

if [ -x "$STATUS_BUILD" ]; then
  tmp="$(mktemp -d)"
  artifacts="$tmp/artifacts"
  status="$tmp/status.json"

  make_complete_artifacts "$artifacts"
  if GITHUB_SHA=deadbeef GITHUB_RUN_ID=123 "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e '.commit == "deadbeef" and .run_id == "123" and .complete == true
      and ([.shards[].complete] | all)' "$status" >/dev/null; then
    pass "status builder marks a fully valid artifact set complete"
  else
    fail "status builder must mark a fully valid artifact set complete"
  fi

  make_complete_artifacts "$artifacts"
  rm -f "$artifacts/mutation-go-agentapi/server/mutation-report-go-agentapi.json"
  if "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e '.complete == false and .shards["go-agentapi"] == {complete:false,reason:"missing"}' "$status" >/dev/null; then
    pass "status builder reports a missing Go shard without failing to emit JSON"
  else
    fail "status builder must report a missing Go shard"
  fi

  make_complete_artifacts "$artifacts"
  printf '%s' '{"end_time":null,"caught":10,"missed":1,"timeout":0,"unviable":0}' \
    >"$artifacts/mutation-rust-3/agent/mutants.out/outcomes.json"
  if "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e '.complete == false and .shards["rust-3"] == {complete:false,reason:"invalid"}' "$status" >/dev/null; then
    pass "status builder rejects end_time:null as an invalid Rust shard"
  else
    fail "status builder must reject end_time:null"
  fi

  make_complete_artifacts "$artifacts"
  printf '%s' '{"files":[]}' >"$artifacts/mutation-web/web/reports/mutation/mutation.json"
  if "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e '.complete == false and .shards.web == {complete:false,reason:"invalid"}' "$status" >/dev/null; then
    pass "status builder validates the Web reporter shape"
  else
    fail "status builder must reject an invalid Web reporter shape"
  fi
  rm -rf "$tmp"
else
  fail "scripts/mutation-status-build.sh must exist and be executable"
fi

if [ -x "$STATUS_PUSH" ]; then
  pass "scripts/mutation-status-vm-push.sh exists and is executable"
else
  fail "scripts/mutation-status-vm-push.sh must exist and be executable"
fi

# --- Summarizer error propagation (single clear error, no jq noise) -----------

if [ -x "$SUMMARIZE" ]; then
  tmp="$(mktemp -d)"
  printf '%s' '{"caught":10,"missed":1,"timeout":0,"unviable":2}' >"$tmp/rust.json"
  printf '%s' '{"files":{"a.ts":{"mutants":[{"status":"Killed"},{"status":"Survived"}]}}}' >"$tmp/web.json"
  code=0
  out="$(RUST_OUTCOMES="$tmp/rust.json" WEB_REPORT="$tmp/web.json" GO_REPORT="$tmp/NOPE.json" \
    HISTORY_FILE="$tmp/NOHIST" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "2" ] \
    && printf '%s\n' "$out" | grep -q 'missing:' \
    && ! printf '%s\n' "$out" | grep -q 'invalid JSON'; then
    pass "summarizer reports a single clear error on missing input (exit 2, no jq noise)"
  else
    fail "summarizer must emit one clear error on missing input and exit 2 (got code=$code, out=$out)"
  fi
  rm -rf "$tmp"
else
  fail "scripts/mutation-summarize.sh must exist and be executable"
fi

# --- Summarizer drop-rule fires only when a previous baseline is supplied -----
# The drop-rule ("score fell >2pp from the previous run") is dead in CI unless
# HISTORY_FILE carries a prior row: the in-repo history file was retired, so
# previous_row is null and only the <85 floor ever trips. mutation-baseline-fetch.sh
# restores that row from VM; these cases pin the behavior it re-enables. web is
# kept ABOVE the 85 floor so ONLY the drop-rule can catch it.
web_report() { # $1=killed $2=survived → Stryker-shaped JSON at killed/(killed+survived)%
  jq -nc --argjson k "$1" --argjson s "$2" \
    '{files:{"a.ts":{mutants:
       ([range(0; $k) | {status: "Killed"}] + [range(0; $s) | {status: "Survived"}])}}}'
}

if [ -x "$SUMMARIZE" ]; then
  tmp="$(mktemp -d)"
  printf '%s' '{"caught":95,"missed":5,"timeout":0,"unviable":0}' >"$tmp/rust.json"                                    # 95.0
  printf '%s' '{"mutants_killed":95,"mutants_lived":5,"mutants_not_covered":0,"mutants_not_viable":0}' >"$tmp/go.json" # 95.0
  web_report 87 13 >"$tmp/web.json"                                                                                    # 87.0 (> floor)

  # prev web 89.5 → curr 87.0 = 2.5pp drop (> 2pp); rust/go flat.
  printf '%s\n' '{"scores":{"rust":{"score_pct":95.0},"go":{"score_pct":95.0},"web":{"score_pct":89.5}}}' >"$tmp/hist-drop.jsonl"
  code=0
  out="$(RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" WEB_REPORT="$tmp/web.json" \
    HISTORY_FILE="$tmp/hist-drop.jsonl" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "1" ] \
    && printf '%s\n' "$out" | grep -q '(drop > 2pp)' \
    && printf '%s\n' "$out" | grep -q 'WEB:' \
    && ! printf '%s\n' "$out" | grep -q 'below 85% floor'; then
    pass "drop-rule fires on a >2pp fall from the restored baseline (above the floor)"
  else
    fail "drop-rule must fire (exit 1, '(drop > 2pp)') on a >2pp baseline fall (code=$code, out=$out)"
  fi

  # prev web 88.5 → curr 87.0 = 1.5pp drop (< 2pp): no regression.
  printf '%s\n' '{"scores":{"rust":{"score_pct":95.0},"go":{"score_pct":95.0},"web":{"score_pct":88.5}}}' >"$tmp/hist-nodrop.jsonl"
  code=0
  out="$(RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" WEB_REPORT="$tmp/web.json" \
    HISTORY_FILE="$tmp/hist-nodrop.jsonl" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "0" ]; then
    pass "drop-rule stays silent on a <2pp fall from the restored baseline"
  else
    fail "a <2pp fall must not be flagged (code=$code, out=$out)"
  fi

  # Alert branch label derives from GITHUB_REF_NAME (the failing run was the
  # scheduled MAIN run, previously mislabeled 'dev').
  out="$(GITHUB_REF_NAME=main RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" \
    WEB_REPORT="$tmp/web.json" HISTORY_FILE="$tmp/hist-drop.jsonl" "$SUMMARIZE" 2>&1)" || true
  if printf '%s\n' "$out" | grep -q 'regression on main'; then
    pass "alert branch label derives from GITHUB_REF_NAME"
  else
    fail "alert header must say 'regression on main' when GITHUB_REF_NAME=main (out=$out)"
  fi

  out="$(env -u GITHUB_REF_NAME RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" \
    WEB_REPORT="$tmp/web.json" HISTORY_FILE="$tmp/hist-drop.jsonl" "$SUMMARIZE" 2>&1)" || true
  if printf '%s\n' "$out" | grep -q 'regression on dev'; then
    pass "alert branch label falls back to dev when GITHUB_REF_NAME is unset"
  else
    fail "alert header must fall back to 'regression on dev' when GITHUB_REF_NAME is unset (out=$out)"
  fi
  rm -rf "$tmp"
else
  fail "scripts/mutation-summarize.sh must exist and be executable"
fi

# --- Workflow wires the VM baseline restore before Summarize ------------------
# The fetch needs kubectl, so OCI+kube setup must precede the Restore step, and
# Restore must precede Summarize so previous_row sees the reconstructed row.
line_of() { grep -nE "$1" "$WORKFLOW" | head -1 | cut -d: -f1; }
oci_line="$(line_of 'uses:[[:space:]]*\./\.github/actions/oci-kube-setup')"
fetch_line="$(line_of 'mutation-baseline-fetch\.sh')"
summ_line="$(line_of 'mutation-summarize\.sh')"

if [ -n "$fetch_line" ] && [ -n "$summ_line" ] && [ "$fetch_line" -lt "$summ_line" ]; then
  pass "workflow restores the VM baseline before Summarize"
else
  fail "workflow must run mutation-baseline-fetch.sh before mutation-summarize.sh (fetch=$fetch_line summ=$summ_line)"
fi

if [ -n "$oci_line" ] && [ -n "$fetch_line" ] && [ "$oci_line" -lt "$fetch_line" ]; then
  pass "OCI + kube setup precedes the baseline restore (fetch needs kubectl)"
else
  fail "oci-kube-setup must precede the baseline restore (oci=$oci_line fetch=$fetch_line)"
fi

if [ "$(grep -cE 'uses:[[:space:]]*\./\.github/actions/oci-kube-setup' "$WORKFLOW")" = "1" ]; then
  pass "OCI + kube setup is moved, not duplicated"
else
  fail "workflow must contain exactly one oci-kube-setup step (moved ahead of Restore, not duplicated)"
fi

if grep -qE 'VM_EXCLUDE_COMMIT:[[:space:]]*\$\{\{[[:space:]]*github\.sha' "$WORKFLOW"; then
  pass "baseline restore excludes the current commit (VM_EXCLUDE_COMMIT=github.sha)"
else
  fail "restore step must set VM_EXCLUDE_COMMIT to github.sha"
fi

status_build_line="$(line_of 'mutation-status-build\.sh')"
status_upload_line="$(line_of 'name:[[:space:]]*Upload mutation run status')"
status_push_line="$(line_of 'mutation-status-vm-push\.sh')"
incomplete_line="$(line_of 'name:[[:space:]]*Fail incomplete mutation run')"

if [ -n "$status_build_line" ] && [ -n "$status_upload_line" ] \
  && [ "$status_build_line" -lt "$status_upload_line" ] \
  && grep -A5 -E 'name:[[:space:]]*Upload mutation run status' "$WORKFLOW" | grep -qE 'if:[[:space:]]*always\(\)' \
  && grep -A8 -E 'name:[[:space:]]*Upload mutation run status' "$WORKFLOW" | grep -qE 'name:[[:space:]]*mutation-run-status'; then
  pass "workflow builds then always uploads mutation-run-status"
else
  fail "workflow must build status before an if:always mutation-run-status upload"
fi

if [ -n "$oci_line" ] && [ -n "$status_push_line" ] \
  && [ "$oci_line" -lt "$status_push_line" ] && [ "$status_push_line" -lt "$summ_line" ]; then
  pass "workflow pushes completion metrics after OCI setup and before Summarize"
else
  fail "status VM push order is wrong (oci=$oci_line push=$status_push_line summarize=$summ_line)"
fi

if [ -n "$incomplete_line" ] && [ -n "$summ_line" ] && [ "$incomplete_line" -lt "$summ_line" ] \
  && grep -A5 -E 'name:[[:space:]]*Fail incomplete mutation run' "$WORKFLOW" \
  | grep -q "steps.status.outputs.complete != 'true'"; then
  pass "workflow fails an incomplete run before canonical summarization"
else
  fail "workflow needs an explicit status-gated incomplete-run failure"
fi

canonical_guards=0
for step_name in 'Upload canonical row as artifact' 'Push to VictoriaMetrics'; do
  if grep -A4 -E "name:[[:space:]]*$step_name" "$WORKFLOW" \
    | grep -q "steps.status.outputs.complete == 'true'"; then
    canonical_guards=$((canonical_guards + 1))
  fi
done
if [ "$canonical_guards" -eq 2 ]; then
  pass "canonical artifact and VM score push are both complete-status gated"
else
  fail "canonical upload/push need explicit complete-status guards (found=$canonical_guards)"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
