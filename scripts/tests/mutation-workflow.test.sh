#!/usr/bin/env bash
# Tests for the mutation workflow: exit-code classification, the Go source partition,
# shard-report merge, run-status generation and summarizer error propagation.

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

# The 90-minute ceiling sits above the widest measured shard, since a shard cut at the ceiling
# reports nothing.
if grep -qE "^[[:space:]]*timeout-minutes:[[:space:]]*90[[:space:]]*$" "$WORKFLOW"; then
  pass "mutation job timeout is a flat 90 minutes (every sharded leg fits under it)"
else
  fail "mutation job must set timeout-minutes: 90"
fi

if grep -qE 'mutation_rust_shard_args' "$WORKFLOW"; then
  pass "rust step selects its mutants through the shard map"
else
  fail "rust step must select mutants via mutation_rust_shard_args"
fi

# cargo-mutants runs the package's whole suite once per mutant, so the config skips the
# multi-second reach measurement in mesh-agent-core.
MUTANTS_TOML="$REPO_ROOT/agent/.cargo/mutants.toml"
if [ -f "$MUTANTS_TOML" ] \
  && grep -q 'additional_cargo_test_args' "$MUTANTS_TOML" \
  && grep -q -- '--skip' "$MUTANTS_TOML" \
  && grep -q 'the_minute_tier_reaches_back_far_enough_to_be_worth_scanning' "$MUTANTS_TOML"; then
  pass "cargo-mutants config keeps the multi-second reach measurement out of every mutant's test run"
else
  fail "agent/.cargo/mutants.toml must skip the reach measurement via additional_cargo_test_args"
fi

REACH_TEST="$REPO_ROOT/agent/crates/mesh-agent-core/tests/reach_test.rs"
if [ -f "$REACH_TEST" ] \
  && grep -q 'fn the_minute_tier_reaches_back_far_enough_to_be_worth_scanning' "$REACH_TEST"; then
  pass "the reach measurement still runs in the normal test suite"
else
  fail "reach_test.rs must keep the_minute_tier_reaches_back_far_enough_to_be_worth_scanning"
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

if [ -f "$SHARDS_LIB" ]; then
  # shellcheck source=/dev/null
  . "$SHARDS_LIB"
  pass "scripts/lib/mutation-shards.sh exists and sources cleanly"

  want_ids="$(mutation_go_shards | tr ' ' '\n' | sort | tr '\n' ' ')"
  have_ids="$(grep -oE 'shard:[[:space:]]*go-[a-z0-9-]+' "$WORKFLOW" \
    | sed -E 's/shard:[[:space:]]*//' | sort -u | tr '\n' ' ')"
  if [ "$want_ids" = "$have_ids" ]; then
    pass "workflow matrix Go shard ids match the shard map"
  else
    fail "workflow shard ids drifted from map: map='$want_ids' wf='$have_ids'"
  fi

  have_rust="$(mutation_rust_shards | tr ' ' '\n' | sort | tr '\n' ' ')"
  meaningful_rust="rust-agent-loops rust-core-alerts-conditions rust-core-alerts-evaluator rust-core-alerts-event rust-core-alerts-retro-plan rust-core-alerts-retro-scan rust-core-alerts-sink rust-core-correlate-divergence rust-core-correlate-ranking rust-core-discovery rust-core-ml-analysis rust-core-ml-backfill-drain rust-core-ml-backfill-tiers rust-core-ml-host-sources rust-core-ml-redaction rust-core-ml-sampling rust-core-ml-store-sink rust-core-runtime rust-core-runtime-lifecycle rust-core-session-dispatch rust-core-session-terminal rust-protocol-wire rust-tsdb-blocks rust-tsdb-encoding rust-tsdb-surface "
  if [ "$have_rust" = "$meaningful_rust" ]; then
    pass "Rust shard ids describe their owned behavior"
  else
    fail "Rust shard ids must describe behavior (got='$have_rust')"
  fi

  workflow_rust="$({ grep -oE 'shard:[[:space:]]*rust-[a-z0-9-]+' "$WORKFLOW" || true; } \
    | sed -E 's/shard:[[:space:]]*//' | sort -u | tr '\n' ' ')"
  if [ "$have_rust" = "$workflow_rust" ]; then
    pass "workflow matrix rust legs match the shard map"
  else
    fail "rust matrix drifted from map (want='$have_rust' got='$workflow_rust')"
  fi

  read -r -a rust_shards <<<"$(mutation_rust_shards)"

  rust_pkg_bad=""
  declare -A rust_catchall=()
  declare -A rust_pkg_seen=()
  for shard in "${rust_shards[@]}"; do
    pkg="$(mutation_rust_shard_package "$shard")" \
      || {
        rust_pkg_bad="$rust_pkg_bad [$shard:no-package]"
        continue
      }
    [ -d "$REPO_ROOT/agent/crates/$pkg/src" ] \
      || rust_pkg_bad="$rust_pkg_bad [$shard:no-such-package:$pkg]"
    rust_pkg_seen[$pkg]=1
    if [ "$(mutation_rust_shard_units "$shard")" = "rest" ]; then
      [ -n "${rust_catchall[$pkg]:-}" ] \
        && rust_pkg_bad="$rust_pkg_bad [$pkg:two-catch-alls:$shard+${rust_catchall[$pkg]}]"
      rust_catchall[$pkg]="$shard"
    fi
  done
  for pkg in "${!rust_pkg_seen[@]}"; do
    [ -n "${rust_catchall[$pkg]:-}" ] || rust_pkg_bad="$rust_pkg_bad [$pkg:no-catch-all]"
  done
  if [ -z "$rust_pkg_bad" ]; then
    pass "each Rust package has exactly one catch-all shard"
  else
    fail "Rust shard package map is wrong:$rust_pkg_bad"
  fi

  rust_carved="$(sed -n '/^exclude_globs[[:space:]]*=/,/]/p' "$REPO_ROOT/agent/.cargo/mutants.toml" \
    | grep -oE '"[^"]+"' | tr -d '"')"
  rust_is_carved() {
    local source="$1" glob
    for glob in $rust_carved; do
      case "$glob" in
        */\*\*) [[ "$source" == "${glob%/\*\*}/"* ]] && return 0 ;;
        *) [ "$source" = "$glob" ] && return 0 ;;
      esac
    done
    return 1
  }

  rust_partition_bad=""
  while IFS= read -r source; do
    rel="${source#"$REPO_ROOT/agent/"}"
    rust_is_carved "$rel" && continue
    pkg="$(printf '%s\n' "$rel" | cut -d/ -f2)"
    owners=0
    for shard in "${rust_shards[@]}"; do
      [ "$(mutation_rust_shard_package "$shard")" = "$pkg" ] || continue
      units="$(mutation_rust_shard_units "$shard")"
      [ "$units" = "rest" ] && continue
      for unit in $units; do
        mutation_rust_unit_matches "$unit" "$rel" && owners=$((owners + 1))
      done
    done
    [ "$owners" -eq 0 ] && owners=1
    [ "$owners" -eq 1 ] || rust_partition_bad="$rust_partition_bad [$rel:owners=$owners]"
  done < <(find "$REPO_ROOT/agent/crates" -type f -name '*.rs' -path '*/src/*' | sort)
  if [ -z "$rust_partition_bad" ]; then
    pass "every mutable Rust source is assigned to exactly one shard"
  else
    fail "Rust source partition mismatch:$rust_partition_bad"
  fi

  rust_carved_unit_bad=""
  for shard in "${rust_shards[@]}"; do
    units="$(mutation_rust_shard_units "$shard")"
    [ "$units" = "rest" ] && continue
    for unit in $units; do
      case "$unit" in
        file:*) rust_is_carved "${unit#file:}" && rust_carved_unit_bad="$rust_carved_unit_bad [$shard:$unit]" ;;
        dir:*) rust_is_carved "${unit#dir:}/x.rs" && rust_carved_unit_bad="$rust_carved_unit_bad [$shard:$unit]" ;;
        *) ;;
      esac
    done
  done
  if [ -z "$rust_carved_unit_bad" ]; then
    pass "no Rust shard claims a source cargo-mutants carves out"
  else
    fail "Rust shards claim carved-out sources:$rust_carved_unit_bad"
  fi

  rust_unit_bad=""
  for shard in "${rust_shards[@]}"; do
    units="$(mutation_rust_shard_units "$shard")"
    [ "$units" = "rest" ] && continue
    for unit in $units; do
      case "$unit" in
        dir:*) [ -d "$REPO_ROOT/agent/${unit#dir:}" ] || rust_unit_bad="$rust_unit_bad [$shard:$unit:no-such-dir]" ;;
        file:*) [ -f "$REPO_ROOT/agent/${unit#file:}" ] || rust_unit_bad="$rust_unit_bad [$shard:$unit:no-such-file]" ;;
        *) rust_unit_bad="$rust_unit_bad [$shard:$unit:bad-kind]" ;;
      esac
    done
  done
  if [ -z "$rust_unit_bad" ]; then
    pass "all Rust mutation units use valid dir:/file: paths"
  else
    fail "invalid Rust mutation unit declarations:$rust_unit_bad"
  fi

  rust_args_bad=""
  for shard in "${rust_shards[@]}"; do
    mapfile -t shard_args < <(mutation_rust_shard_args "$shard")
    [ "${shard_args[0]}" = "--package" ] \
      || rust_args_bad="$rust_args_bad [$shard:not-package-scoped]"
    [ "${shard_args[1]}" = "$(mutation_rust_shard_package "$shard")" ] \
      || rust_args_bad="$rust_args_bad [$shard:wrong-package]"
    grep -q -- '--workspace' <<<"$(printf '%s\n' "${shard_args[@]}")" \
      && rust_args_bad="$rust_args_bad [$shard:workspace-wide]"
    if [ "$(mutation_rust_shard_units "$shard")" = "rest" ]; then
      for other in "${rust_shards[@]}"; do
        [ "$other" = "$shard" ] && continue
        [ "$(mutation_rust_shard_package "$other")" = "$(mutation_rust_shard_package "$shard")" ] || continue
        other_units="$(mutation_rust_shard_units "$other")"
        [ "$other_units" = "rest" ] && continue
        for unit in $other_units; do
          grep -qxF "$(mutation_rust_shard_glob "$unit")" <<<"$(printf '%s\n' "${shard_args[@]}")" \
            || rust_args_bad="$rust_args_bad [$shard:missing-exclude:$unit]"
        done
      done
    else
      for unit in $(mutation_rust_shard_units "$shard"); do
        grep -qxF "$(mutation_rust_shard_glob "$unit")" <<<"$(printf '%s\n' "${shard_args[@]}")" \
          || rust_args_bad="$rust_args_bad [$shard:missing-file:$unit]"
      done
    fi
  done
  if [ -z "$rust_args_bad" ]; then
    pass "each Rust shard's cargo-mutants args select exactly its own sources"
  else
    fail "Rust shard args are wrong:$rust_args_bad"
  fi

  if [ "$(mutation_all_shards)" = "$(mutation_rust_shards) $(mutation_go_shards) web" ]; then
    pass "shard library exposes the exact Rust/Go/Web expected set"
  else
    fail "expected shard set drifted (all='$(mutation_all_shards)')"
  fi

  meaningful_go="go-api-runtime go-api-limits go-api-intake go-api-status go-api-converters go-api-identity go-api-tenancy-admin go-api-device-control go-api-device-sessions go-api-device-reads go-api-incidents go-api-rules go-api-enrollment go-api-updates-purge go-agentapi-connection go-agentapi-handshake go-agentapi-backfill go-agentapi-edge-telemetry go-domain-rules go-domain-alerts-room go-domain-alerts-record go-domain-persistence go-amt go-updates-certificates go-protocol-wire go-relay-signaling go-observability-harness go-composition-root"
  if [ "$(mutation_go_shards)" = "$meaningful_go" ]; then
    pass "Go shard ids describe their owned behavior"
  else
    fail "Go shard ids must describe behavior (got='$(mutation_go_shards)')"
  fi

  read -r -a go_shards <<<"$(mutation_go_shards)"
  declare -a all_units=()
  declare -A unit_owner=()
  declare -A shard_regex=()

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

  partition_bad=""
  mutated_bad=""
  declare -A scan_by_shard=()
  declare -A globals_by_shard=()
  for shard in "${go_shards[@]}"; do
    scan_by_shard[$shard]="$(mutation_go_shard_scan_path "$shard")"
    globals_by_shard[$shard]="$(mutation_go_global_excludes "${scan_by_shard[$shard]}")"
  done

  walk_path() {
    local scan="$1" path="$2"
    case "$scan" in
      ./internal)
        case "$path" in
          internal/*) printf '%s' "${path#internal/}" ;;
          *) return 1 ;;
        esac
        ;;
      *) printf '%s' "$path" ;;
    esac
  }

  while IFS= read -r source; do
    rel="${source#"$REPO_ROOT/server/"}"

    matches=0
    for unit in "${all_units[@]}"; do
      if mutation_go_unit_matches "$unit" "$rel"; then
        matches=$((matches + 1))
      fi
    done

    carved=1
    for shard in "${go_shards[@]}"; do
      seen="$(walk_path "${scan_by_shard[$shard]}" "$rel")" || continue
      grep -qE "${globals_by_shard[$shard]}" <<<"$seen" || carved=0
    done

    if [ "$carved" -eq 0 ] && [ "$matches" -ne 1 ]; then
      partition_bad="$partition_bad [$rel:matches=$matches]"
    fi

    mutators=0
    for shard in "${go_shards[@]}"; do
      seen="$(walk_path "${scan_by_shard[$shard]}" "$rel")" || continue
      grep -qE "${shard_regex[$shard]}" <<<"$seen" && continue
      mutators=$((mutators + 1))
      mutated_by="$shard"
    done

    if [ "$carved" -eq 1 ]; then
      [ "$mutators" -eq 0 ] \
        || mutated_bad="$mutated_bad [$rel:carved-but-mutated-by-${mutated_by:-?}]"
    elif [ "$mutators" -ne 1 ]; then
      mutated_bad="$mutated_bad [$rel:mutated-by=$mutators]"
    fi
  done < <(find "$REPO_ROOT/server" -type f -name '*.go' ! -name '*_test.go' | sort)

  if [ -z "$partition_bad" ]; then
    pass "all non-test server Go sources are assigned once or globally excluded"
  else
    fail "whole-server Go source partition mismatch:$partition_bad"
  fi
  if [ -z "$mutated_bad" ]; then
    pass "every server Go source is mutated by exactly one shard's walk and regex"
  else
    fail "Go shard walk/regex mismatch:$mutated_bad"
  fi

  reach_bad=""
  for shard in "${go_shards[@]}"; do
    for unit in $(mutation_go_shard_units "$shard"); do
      walk_path "${scan_by_shard[$shard]}" "${unit#*:}" >/dev/null \
        || reach_bad="$reach_bad [$shard:$unit:outside-its-own-walk]"
    done
  done
  if [ -z "$reach_bad" ]; then
    pass "every Go shard's walk reaches every unit that shard owns"
  else
    fail "a Go shard is pointed at a path that cannot see its own units:$reach_bad"
  fi

  # Under the narrow walk internal/api and internal/agentapi become api/ and agentapi/, so an
  # unanchored rule for api/ would also drop agentapi/.
  anchor_bad=""
  for shard in "${go_shards[@]}"; do
    while IFS= read -r alternative; do
      [ -n "$alternative" ] || continue
      case "$alternative" in
        '^'*) ;;
        'openapi_gen\.go') ;;
        *) anchor_bad="$anchor_bad [$shard:$alternative]" ;;
      esac
    done <<<"$(tr '|' '\n' <<<"${shard_regex[$shard]}")"
  done
  if [ -z "$anchor_bad" ]; then
    pass "every Go shard exclude alternative is anchored at the path's start"
  else
    fail "unanchored Go shard exclude alternatives:$anchor_bad"
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
    [ "$owners" -eq 1 ] && [ "$owner" = "go-observability-harness" ] \
      || loadtest_bad="$loadtest_bad [$f:owners=$owners:$owner]"
  done
  if [ -z "$loadtest_bad" ] \
    && grep -qE "$(mutation_go_global_excludes)" <<<'tests/loadtest/main.go'; then
    pass "loadtest helpers mutate once in go-observability-harness while main.go stays excluded"
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

  agentapi_bad=""
  agentapi_owners=""
  while IFS= read -r source; do
    rel="${source#"$REPO_ROOT/server/"}"
    owners=0
    owner=""
    for unit in "${all_units[@]}"; do
      if mutation_go_unit_matches "$unit" "$rel"; then
        owners=$((owners + 1))
        owner="${unit_owner[$unit]}"
      fi
    done
    case "$owner" in
      go-agentapi-connection | go-agentapi-handshake | go-agentapi-backfill | go-agentapi-edge-telemetry) : ;;
      *) agentapi_bad="$agentapi_bad [$rel:owner=$owner]" ;;
    esac
    [ "$owners" -eq 1 ] || agentapi_bad="$agentapi_bad [$rel:owners=$owners]"
    agentapi_owners="$agentapi_owners $owner"
  done < <(find "$REPO_ROOT/server/internal/agentapi" -maxdepth 1 -type f -name '*.go' ! -name '*_test.go' | sort)
  for required in go-agentapi-connection go-agentapi-handshake go-agentapi-backfill go-agentapi-edge-telemetry; do
    case " $agentapi_owners " in
      *" $required "*) : ;;
      *) agentapi_bad="$agentapi_bad [$required:empty]" ;;
    esac
  done
  if [ -z "$agentapi_bad" ]; then
    pass "agentapi sources are split across four non-empty file-unit shards"
  else
    fail "agentapi file-unit partition mismatch:$agentapi_bad"
  fi

  backfill_unit="file:internal/agentapi/conn_backfill.go"
  handshake_unit="file:internal/agentapi/handshaker.go"
  backfill_owner="${unit_owner[$backfill_unit]:-}"
  handshake_owner="${unit_owner[$handshake_unit]:-}"
  if [ "$backfill_owner" = "go-agentapi-backfill" ] \
    && [ "$handshake_owner" = "go-agentapi-handshake" ] \
    && [ "$backfill_owner" != "$handshake_owner" ]; then
    pass "timeout-heavy agent API backfill and handshake files run in separate shards"
  else
    fail "conn_backfill.go and handshaker.go must be isolated (backfill=$backfill_owner handshake=$handshake_owner)"
  fi

  # The leash is the coverage run's elapsed time times the timeout coefficient, which bounds what
  # a non-terminating mutant costs a shard: the VAPID padding loop and the MPS accept loop.
  baseline_coef="$(sed -nE 's/^[[:space:]]*timeout-coefficient:[[:space:]]*([0-9]+).*/\1/p' "$REPO_ROOT/server/.gremlins.yaml")"
  leash="$(mutation_go_leash_ceiling_seconds)"
  if [ -n "$baseline_coef" ] \
    && [ "$leash" = "$((baseline_coef * $(mutation_go_coverage_elapsed_ceiling_seconds)))" ]; then
    pass "the leash ceiling is the baseline coefficient times the coverage ceiling"
  else
    fail "leash ceiling must derive from server/.gremlins.yaml (coef='$baseline_coef' leash='$leash')"
  fi

  cap_s=$((90 * 60))
  spent=$((cap_s - $(mutation_go_setup_ceiling_seconds) - $(mutation_go_coverage_elapsed_ceiling_seconds) - $(mutation_go_shard_budget_minutes) * 60))
  if [ "$leash" -le "$spent" ]; then
    pass "a non-terminating mutant fits inside the job's remaining headroom"
  else
    fail "leash ceiling ${leash}s exceeds the ${spent}s a fully-spent shard has left"
  fi

  if [ "$leash" -ge $((290 * 2)) ]; then
    pass "the leash leaves a finishing mutant at least twice its measured worst case"
  else
    fail "leash ceiling ${leash}s is too tight for the ~290s slowest finishing mutant"
  fi

  coef_bad=""
  for shard in "${go_shards[@]}"; do
    got="$(mutation_go_shard_timeout_coefficient "$shard")"
    [ -z "$got" ] && continue
    [[ "$got" =~ ^[0-9]+$ ]] && [ "$got" -lt "$baseline_coef" ] && [ "$got" -ge 2 ] \
      || coef_bad="$coef_bad [$shard=$got]"
  done
  if [ -z "$coef_bad" ]; then
    pass "no Go shard overrides the coefficient upward"
  else
    fail "a scoped coefficient must be numeric, >=2 and <baseline($baseline_coef):$coef_bad"
  fi

  if grep -q 'mutation_go_shard_timeout_coefficient' "$WORKFLOW" \
    && grep -q -- '--timeout-coefficient' "$WORKFLOW"; then
    pass "workflow derives --timeout-coefficient from the shard library"
  else
    fail "workflow must derive --timeout-coefficient from mutation_go_shard_timeout_coefficient"
  fi
  if grep -q 'mutation_go_shard_timeout_coefficient' "$REPO_ROOT/Makefile" \
    && grep -q -- '--timeout-coefficient' "$REPO_ROOT/Makefile"; then
    pass "make mutate-go derives --timeout-coefficient from the shard library"
  else
    fail "Makefile mutate-go must derive --timeout-coefficient from the shard library"
  fi

  for caller in "$WORKFLOW" "$REPO_ROOT/Makefile"; do
    name="$(basename "$caller")"
    if grep -q 'mutation_go_shard_scan_path' "$caller" \
      && ! grep -qE 'gremlins unleash (\.|\./internal)[[:space:]]' "$caller"; then
      pass "$name walks the path the shard library names"
    else
      fail "$name must take gremlins' path from mutation_go_shard_scan_path, not spell one out"
    fi
  done

  # The pre-flight output is teed into the step summary, so the step sets pipefail to fail on an
  # OVER shard.
  budget_step="$(sed -n '/Project every shard against the job cap/,/^$/p' "$WORKFLOW")"
  if grep -q 'mutation-shard-budget.sh' <<<"$budget_step" \
    && grep -q 'set -o pipefail' <<<"$budget_step"; then
    pass "the shard-budget step fails when the projection is refused"
  else
    fail "the shard-budget step must set pipefail around mutation-shard-budget.sh"
  fi

  for tool_step in \
    'Run cargo-mutants (shard' \
    'Run gremlins (shard' \
    'Run stryker'; do
    step="$(sed -n "/${tool_step}/,/^      - /p" "$WORKFLOW")"
    if grep -q 'assert-mutation-report.sh' <<<"$step"; then
      pass "the ${tool_step%% (shard*} step reads its own report back"
    else
      fail "the ${tool_step%% (shard*} step must read its report back, not leave it to the upload"
    fi
  done

  budget_job="$(sed -n '/^  shard-budget:/,/^  mutation:/p' "$WORKFLOW")"
  if grep -q 'POSTGRES_TEST_URL' <<<"$budget_job" \
    && grep -q 'gremlins' <<<"$budget_job"; then
    pass "the shard-budget job counts Go mutants against a real Postgres"
  else
    fail "the shard-budget job must install gremlins and give it POSTGRES_TEST_URL"
  fi

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

if grep -q 'mutation-merge-rust\.sh' "$WORKFLOW"; then
  pass "publish merges the rust shards via mutation-merge-rust.sh"
else
  fail "publish must merge rust shards via mutation-merge-rust.sh"
fi

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
  rm -f "$artifacts/mutation-go-agentapi-backfill/server/mutation-report-go-agentapi-backfill.json"
  if "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e '.complete == false and .shards["go-agentapi-backfill"] == {complete:false,reason:"missing"}' "$status" >/dev/null; then
    pass "status builder reports a missing Go shard without failing to emit JSON"
  else
    fail "status builder must report a missing Go shard"
  fi

  make_complete_artifacts "$artifacts"
  probe_rust_shard="$(mutation_rust_shards | awk '{print $3}')"
  printf '%s' '{"end_time":null,"caught":10,"missed":1,"timeout":0,"unviable":0}' \
    >"$artifacts/mutation-$probe_rust_shard/agent/mutants.out/outcomes.json"
  if "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e --arg s "$probe_rust_shard" '.complete == false and .shards[$s] == {complete:false,reason:"invalid"}' "$status" >/dev/null; then
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

  make_complete_artifacts "$artifacts"
  if "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e '.complete_by_language == {rust:true,go:true,web:true}' "$status" >/dev/null; then
    pass "status builder reports every leg complete on a whole artifact set"
  else
    fail "status builder must report completeness per language"
  fi

  make_complete_artifacts "$artifacts"
  rm -f "$artifacts/mutation-go-agentapi-backfill/server/mutation-report-go-agentapi-backfill.json"
  if "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e '.complete == false and .complete_by_language == {rust:true,go:false,web:true}' "$status" >/dev/null; then
    pass "a missing Go shard leaves the Rust and web legs complete"
  else
    fail "a missing Go shard must not mark the other legs incomplete"
  fi

  make_complete_artifacts "$artifacts"
  printf '%s' '{"files":[]}' >"$artifacts/mutation-web/web/reports/mutation/mutation.json"
  if "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e '.complete_by_language == {rust:true,go:true,web:false}' "$status" >/dev/null; then
    pass "an invalid web report leaves the Rust and Go legs complete"
  else
    fail "an invalid web report must not mark the other legs incomplete"
  fi

  make_complete_artifacts "$artifacts"
  rm -f "$artifacts/mutation-go-agentapi-backfill/server/mutation-report-go-agentapi-backfill.json"
  if "$STATUS_BUILD" "$artifacts" "$status" >/dev/null 2>&1 \
    && jq -e '.complete == false' "$status" >/dev/null; then
    pass "complete still means every leg, so its existing readers are unchanged"
  else
    fail "complete changed meaning"
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

if [ -x "$SUMMARIZE" ]; then
  tmp="$(mktemp -d)"
  printf '%s' '{"caught":10,"missed":1,"timeout":0,"unviable":2}' >"$tmp/rust.json"
  printf '%s' '{"files":{"a.ts":{"mutants":[{"status":"Killed"},{"status":"Survived"}]}}}' >"$tmp/web.json"
  code=0
  out="$(RUST_OUTCOMES="$tmp/rust.json" WEB_REPORT="$tmp/web.json" GO_REPORT="$tmp/NOPE.json" \
    HISTORY_FILE="$tmp/NOHIST" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "2" ] \
    && grep -q 'missing:' <<<"$out" \
    && ! grep -q 'invalid JSON' <<<"$out"; then
    pass "summarizer reports a single clear error on missing input (exit 2, no jq noise)"
  else
    fail "summarizer must emit one clear error on missing input and exit 2 (got code=$code, out=$out)"
  fi
  rm -rf "$tmp"
else
  fail "scripts/mutation-summarize.sh must exist and be executable"
fi

# The drop rule (score fell >2pp from the previous run) needs HISTORY_FILE to carry a prior row.
# The web score stays above the 85 floor so only the drop rule can catch it.
web_report() { # $1=killed $2=survived → Stryker-shaped JSON at killed/(killed+survived)%
  jq -nc --argjson k "$1" --argjson s "$2" \
    '{files:{"a.ts":{mutants:
       ([range(0; $k) | {status: "Killed"}] + [range(0; $s) | {status: "Survived"}])}}}'
}

if [ -x "$SUMMARIZE" ]; then
  tmp="$(mktemp -d)"
  printf '%s' '{"caught":95,"missed":5,"timeout":0,"unviable":0}' >"$tmp/rust.json"
  printf '%s' '{"mutants_killed":95,"mutants_lived":5,"mutants_not_covered":0,"mutants_not_viable":0}' >"$tmp/go.json"
  web_report 87 13 >"$tmp/web.json"

  printf '%s\n' '{"scores":{"rust":{"score_pct":95.0},"go":{"score_pct":95.0},"web":{"score_pct":89.5}}}' >"$tmp/hist-drop.jsonl"
  code=0
  out="$(RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" WEB_REPORT="$tmp/web.json" \
    HISTORY_FILE="$tmp/hist-drop.jsonl" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "1" ] \
    && grep -q '(drop > 2pp)' <<<"$out" \
    && grep -q 'WEB:' <<<"$out" \
    && ! grep -q 'below 85% floor' <<<"$out"; then
    pass "drop-rule fires on a >2pp fall from the restored baseline (above the floor)"
  else
    fail "drop-rule must fire (exit 1, '(drop > 2pp)') on a >2pp baseline fall (code=$code, out=$out)"
  fi

  printf '%s\n' '{"scores":{"rust":{"score_pct":95.0},"go":{"score_pct":95.0},"web":{"score_pct":88.5}}}' >"$tmp/hist-nodrop.jsonl"
  code=0
  out="$(RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" WEB_REPORT="$tmp/web.json" \
    HISTORY_FILE="$tmp/hist-nodrop.jsonl" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "0" ]; then
    pass "drop-rule stays silent on a <2pp fall from the restored baseline"
  else
    fail "a <2pp fall must not be flagged (code=$code, out=$out)"
  fi

  out="$(GITHUB_REF_NAME=main RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" \
    WEB_REPORT="$tmp/web.json" HISTORY_FILE="$tmp/hist-drop.jsonl" "$SUMMARIZE" 2>&1)" || true
  if grep -q 'regression on main' <<<"$out"; then
    pass "alert branch label derives from GITHUB_REF_NAME"
  else
    fail "alert header must say 'regression on main' when GITHUB_REF_NAME=main (out=$out)"
  fi

  out="$(env -u GITHUB_REF_NAME RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" \
    WEB_REPORT="$tmp/web.json" HISTORY_FILE="$tmp/hist-drop.jsonl" "$SUMMARIZE" 2>&1)" || true
  if grep -q 'regression on dev' <<<"$out"; then
    pass "alert branch label falls back to dev when GITHUB_REF_NAME is unset"
  else
    fail "alert header must fall back to 'regression on dev' when GITHUB_REF_NAME is unset (out=$out)"
  fi
  rm -rf "$tmp"
else
  fail "scripts/mutation-summarize.sh must exist and be executable"
fi

if [ -x "$SUMMARIZE" ]; then
  tmp="$(mktemp -d)"
  printf '%s' '{"caught":95,"missed":5,"timeout":0,"unviable":0}' >"$tmp/rust.json"
  printf '%s' '{"mutants_killed":95,"mutants_lived":5,"mutants_not_covered":0,"mutants_not_viable":0}' >"$tmp/go.json"
  web_report 95 5 >"$tmp/web.json"

  code=0
  out="$(MUTATION_LANGUAGES="rust web" RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/NOPE.json" \
    WEB_REPORT="$tmp/web.json" HISTORY_FILE="$tmp/NOHIST" "$SUMMARIZE" 2>&1)" || code=$?
  rows="$(grep -E '^\{' <<<"$out" || true)"
  row="${rows##*$'\n'}"
  if [ "$code" = "0" ] && jq -e '
      (.scores | keys) == ["rust","web"]
      and .scores.rust.score_pct == 95
      and (.scores | has("go") | not)' <<<"$row" >/dev/null 2>&1; then
    pass "a row asked for two legs carries two legs and no absent third"
  else
    fail "the summarizer must carry only the legs it was asked for (code=$code, out=$out)"
  fi

  code=0
  out="$(MUTATION_LANGUAGES="go" RUST_OUTCOMES="$tmp/NOPE.json" GO_REPORT="$tmp/go.json" \
    WEB_REPORT="$tmp/NOPE.json" HISTORY_FILE="$tmp/NOHIST" "$SUMMARIZE" 2>&1)" || code=$?
  rows="$(grep -E '^\{' <<<"$out" || true)"
  row="${rows##*$'\n'}"
  if [ "$code" = "0" ] && jq -e '(.scores | keys) == ["go"]' <<<"$row" >/dev/null 2>&1; then
    pass "a single-leg row is a row"
  else
    fail "the summarizer must publish a single complete leg (code=$code, out=$out)"
  fi

  printf '%s' 'not json at all' >"$tmp/broken.json"
  code=0
  out="$(MUTATION_LANGUAGES="rust go" RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/broken.json" \
    WEB_REPORT="$tmp/NOPE.json" HISTORY_FILE="$tmp/NOHIST" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "2" ]; then
    pass "a leg asked for and malformed is still exit 2"
  else
    fail "a malformed leg that was asked for must exit 2, not publish (code=$code, out=$out)"
  fi

  code=0
  out="$(MUTATION_LANGUAGES="" RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" \
    WEB_REPORT="$tmp/web.json" HISTORY_FILE="$tmp/NOHIST" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "2" ]; then
    pass "a run naming no complete leg is refused"
  else
    fail "an empty language set must be refused (code=$code, out=$out)"
  fi

  code=0
  out="$(MUTATION_LANGUAGES="rust perl" RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" \
    WEB_REPORT="$tmp/web.json" HISTORY_FILE="$tmp/NOHIST" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "2" ]; then
    pass "a language the summarizer does not know is refused"
  else
    fail "an unknown language must be refused (code=$code, out=$out)"
  fi

  printf '%s\n' '{"scores":{"rust":{"score_pct":95.0},"go":{"score_pct":95.0},"web":{"score_pct":89.5}}}' >"$tmp/hist.jsonl"
  web_report 87 13 >"$tmp/web-drop.json"
  code=0
  out="$(MUTATION_LANGUAGES="web" RUST_OUTCOMES="$tmp/NOPE.json" GO_REPORT="$tmp/NOPE.json" \
    WEB_REPORT="$tmp/web-drop.json" HISTORY_FILE="$tmp/hist.jsonl" "$SUMMARIZE" 2>&1)" || code=$?
  if [ "$code" = "1" ] && grep -q 'WEB:' <<<"$out" && ! grep -q 'GO:' <<<"$out"; then
    pass "a leg that ran is still regression-checked when the others did not"
  else
    fail "the regression check must read the legs present and name no others (code=$code, out=$out)"
  fi

  code=0
  out="$(RUST_OUTCOMES="$tmp/rust.json" GO_REPORT="$tmp/go.json" WEB_REPORT="$tmp/web.json" \
    HISTORY_FILE="$tmp/NOHIST" "$SUMMARIZE" 2>&1)" || code=$?
  rows="$(grep -E '^\{' <<<"$out" || true)"
  row="${rows##*$'\n'}"
  if [ "$code" = "0" ] && jq -e '(.scores | keys) == ["go","rust","web"]' <<<"$row" >/dev/null 2>&1; then
    pass "naming no subset still publishes all three legs"
  else
    fail "the default must stay all three legs (code=$code, out=$out)"
  fi
  rm -rf "$tmp"
else
  fail "scripts/mutation-summarize.sh must exist and be executable"
fi

# line_of prints the first line number matching a pattern, or nothing. It avoids `grep | head`,
# which under pipefail ends the script on no match (.claude/rules/assertion-determinism.md).
line_of() {
  local matches
  matches="$(grep -nE "$1" "$WORKFLOW" || true)"
  [ -n "$matches" ] || return 0
  cut -d: -f1 <<<"${matches%%$'\n'*}"
}
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

if grep -qE 'VM_RUN_STARTED_AT:[[:space:]]*\$\{\{[[:space:]]*needs\.shard-budget\.outputs\.started_at' "$WORKFLOW" \
  && ! grep -qF 'VM_EXCLUDE_COMMIT' "$WORKFLOW"; then
  pass "baseline restore reads the previous night by date (VM_RUN_STARTED_AT from the first job)"
else
  fail "the publish job must hand the reader VM_RUN_STARTED_AT from shard-budget, and exclude nothing by commit"
fi

status_build_line="$(line_of 'mutation-status-build\.sh')"
status_upload_line="$(line_of 'name:[[:space:]]*Upload mutation run status')"
status_push_line="$(line_of 'mutation-status-vm-push\.sh')"
incomplete_line="$(line_of 'name:[[:space:]]*Fail a mutation run with no complete leg')"

# The step's following lines go into variables so `grep -q` cannot lose a match to a failed pipe.
upload_next5="$(grep -A5 -E 'name:[[:space:]]*Upload mutation run status' "$WORKFLOW" || true)"
upload_next8="$(grep -A8 -E 'name:[[:space:]]*Upload mutation run status' "$WORKFLOW" || true)"

if [ -n "$status_build_line" ] && [ -n "$status_upload_line" ] \
  && [ "$status_build_line" -lt "$status_upload_line" ] \
  && grep -qE 'if:[[:space:]]*always\(\)' <<<"$upload_next5" \
  && grep -qE 'name:[[:space:]]*mutation-run-status' <<<"$upload_next8"; then
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

incomplete_step="$(sed -n "/name:[[:space:]]*Fail a mutation run with no complete leg/,+5p" "$WORKFLOW")"
if [ -n "$incomplete_line" ] && [ -n "$summ_line" ] && [ "$incomplete_line" -lt "$summ_line" ] \
  && grep -q "steps.status.outputs.complete-legs == ''" <<<"$incomplete_step"; then
  pass "workflow fails a run with no complete leg before canonical summarization"
else
  fail "workflow needs an explicit failure when no leg completed"
fi

canonical_guards=0
for step_name in 'Upload canonical row as artifact' 'Push to VictoriaMetrics'; do
  step="$(sed -n "/name:[[:space:]]*$step_name/,+4p" "$WORKFLOW")"
  if grep -q "steps.status.outputs.complete-legs != ''" <<<"$step"; then
    canonical_guards=$((canonical_guards + 1))
  fi
done
if [ "$canonical_guards" -eq 2 ]; then
  pass "canonical artifact and VM score push publish whatever legs completed"
else
  fail "canonical upload/push must be gated on the complete legs, not the whole set (found=$canonical_guards)"
fi

summarize_step="$(sed -n "/name:[[:space:]]*Summarize + regression check/,+8p" "$WORKFLOW")"
if grep -qE 'MUTATION_LANGUAGES: .*steps\.status\.outputs\.complete-legs' <<<"$summarize_step"; then
  pass "the summarizer is handed the legs that completed"
else
  fail "the summarize step must hand mutation-summarize.sh the complete legs"
fi

gate_step="$(sed -n "/name:[[:space:]]*Fail workflow red on regression/,/^  [a-z-]*:$/p" "$WORKFLOW")"
if grep -q 'needs.publish.outputs.complete' <<<"$gate_step" \
  && grep -q 'needs.publish.outputs.regression' <<<"$gate_step"; then
  pass "the gate still fails a night any leg missed, and a night any leg regressed"
else
  fail "the gate must fail on an incomplete run as well as on a regression"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
