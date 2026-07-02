#!/usr/bin/env bash
# Tests for the mutation workflow: timeout/exit-code classification, the Go
# shard partition, the shard-report merge, and summarizer error propagation.
#
# Bug history: GitHub Actions run 27743482464 cancelled the Go gremlins leg at
# the job cap before server/mutation-report.json could be uploaded; the publish
# job then collapsed mutation-summarize.sh exit 2 (missing input) into
# "regression=1", mislabeling an incomplete run as a score regression. Exit-code
# semantics are pinned here: 0 = clean, 1 = score regression, 2 =
# incomplete/malformed input.
#
# Scaling: the Go leg is sharded by package (td-gremlins-timeout-stability.md) so
# it no longer crosses the cap as the server grows. The shard split lives in one
# place (scripts/lib/mutation-shards.sh); these tests assert the workflow matrix
# matches it and that the shards partition every non-excluded internal package.
#
# Run: ./scripts/tests/mutation-workflow.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOW="$REPO_ROOT/.github/workflows/mutation.yml"
SHARDS_LIB="$REPO_ROOT/scripts/lib/mutation-shards.sh"
MERGE="$REPO_ROOT/scripts/mutation-merge-go.sh"
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

# The job timeout defaults to 75 minutes (Go/web legs) and lets a matrix entry
# override it (rust carries a higher cap since the Edge Sentinel ML crates grew
# its unsharded workspace run past 75min).
if grep -qE "^[[:space:]]*timeout-minutes:[[:space:]]*\\\$\{\{[[:space:]]*matrix\.timeout-minutes[[:space:]]*\|\|[[:space:]]*75[[:space:]]*\}\}" "$WORKFLOW"; then
  pass "mutation job timeout defaults to 75 minutes with per-leg override"
else
  fail "mutation job timeout must default to 75 minutes (matrix.timeout-minutes override)"
fi

if grep -qE '^[[:space:]]*-[[:space:]]*\{[[:space:]]*language:[[:space:]]*rust,[[:space:]]*timeout-minutes:[[:space:]]*[0-9]+' "$WORKFLOW"; then
  pass "rust mutation leg carries its own timeout override"
else
  fail "rust mutation leg must set a matrix.timeout-minutes override above the 75min default"
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

  # Every non-excluded server/internal/<pkg> is in exactly one shard, and no
  # shard names a package that does not exist or is excluded.
  declare -A in_shard=()
  dup=""
  for shard in $(mutation_go_shards); do
    for pkg in $(mutation_go_shard_pkgs "$shard"); do
      if [ -n "${in_shard[$pkg]:-}" ]; then
        dup="$dup $pkg"
      fi
      in_shard[$pkg]="$shard"
    done
  done

  declare -A excluded=()
  for pkg in $(mutation_go_excluded_pkgs); do excluded[$pkg]=1; done

  missing=""
  extra=""
  for d in "$REPO_ROOT"/server/internal/*/; do
    pkg="$(basename "$d")"
    if [ -n "${excluded[$pkg]:-}" ]; then
      if [ -n "${in_shard[$pkg]:-}" ]; then
        extra="$extra $pkg(excluded-but-assigned)"
      fi
      continue
    fi
    [ -n "${in_shard[$pkg]:-}" ] || missing="$missing $pkg"
  done
  # Reverse: shard names a package with no directory.
  for pkg in "${!in_shard[@]}"; do
    [ -d "$REPO_ROOT/server/internal/$pkg" ] || extra="$extra $pkg(no-such-package)"
  done

  if [ -z "$missing" ] && [ -z "$extra" ] && [ -z "$dup" ]; then
    pass "shards partition every non-excluded internal/* package exactly once"
  else
    fail "shard partition mismatch:${missing:+ missing[$missing]}${extra:+ extra[$extra]}${dup:+ duplicate[$dup]}"
  fi

  # Workflow matrix go shard ids must match the shard map (no drift).
  want_ids="$(mutation_go_shards | tr ' ' '\n' | sort | tr '\n' ' ')"
  have_ids="$(grep -oE 'shard:[[:space:]]*go-[a-z0-9]+' "$WORKFLOW" \
    | sed -E 's/shard:[[:space:]]*//' | sort -u | tr '\n' ' ')"
  if [ "$want_ids" = "$have_ids" ]; then
    pass "workflow matrix go shard ids match the shard map"
  else
    fail "workflow shard ids drifted from map: map='$want_ids' wf='$have_ids'"
  fi

  # Each shard's exclude regex must exclude every OTHER shard's package, must NOT
  # exclude its own packages, and must exclude the global carve-outs (testutil).
  regex_bad=""
  for shard in $(mutation_go_shards); do
    excl="$(mutation_go_shard_exclude_regex "$shard")"
    printf 'internal/testutil/x.go\n' | grep -qE "$excl" \
      || regex_bad="$regex_bad [$shard:testutil-not-excluded]"
    for pkg in $(mutation_go_shard_pkgs "$shard"); do
      if printf 'internal/%s/x.go\n' "$pkg" | grep -qE "$excl"; then
        regex_bad="$regex_bad [$shard:own-pkg-$pkg-excluded]"
      fi
    done
    for other in $(mutation_go_shards); do
      [ "$other" = "$shard" ] && continue
      for pkg in $(mutation_go_shard_pkgs "$other"); do
        printf 'internal/%s/x.go\n' "$pkg" | grep -qE "$excl" \
          || regex_bad="$regex_bad [$shard:other-pkg-$pkg-not-excluded]"
      done
    done
  done
  if [ -z "$regex_bad" ]; then
    pass "shard exclude regex scopes mutation to its packages (globals re-excluded)"
  else
    fail "shard exclude regex wrong:$regex_bad"
  fi

  # Global excludes must stay in sync with server/.gremlins.yaml exclude-files.
  globals="$(mutation_go_global_excludes)"
  sync_bad=""
  while IFS= read -r pat; do
    # YAML stores doubled backslashes; collapse to a single-backslash regex.
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
  rm -rf "$tmp"
else
  fail "scripts/mutation-merge-go.sh must exist and be executable"
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

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
