#!/usr/bin/env bash
# sonar-duplication-guard.sh — local guardrail against the SonarCloud
# "new_duplicated_lines_density > 3" gate failing only in CI.
#
# The gate fails when duplicated lines among *new* code exceed 3%. SonarCloud
# derives "new" from git blame, so during a pre-commit `make sonar` — which runs
# BEFORE the commit exists — freshly added/changed lines carry no commit date and
# are under-counted as new. A copy-pasted new file can therefore read 0% new
# duplication locally and then, once committed and re-scanned in CI, cross the 3%
# gate: green locally, red in CI (observed: redb_compact.rs, a near-copy of
# redb_store.rs, passed local sonar at commit 6cd5d84 and failed CI run 29071122300).
#
# This guard sidesteps the blame gap by reading each changed file's ABSOLUTE
# duplicated_lines_density, which SonarCloud computes from file content (its
# copy-paste detector), not from blame — so it is reported reliably even for
# uncommitted lines. It runs AFTER `make sonar` has uploaded the working tree and
# fails when any changed source file's whole-file duplication exceeds the ceiling.
#
# The whole repo sits at ~0% file-level duplication, so this is a superset of the
# gate that in practice only fires on the duplication the gate itself would catch;
# a touched file with accepted pre-existing duplication is the one false-positive
# shape, and it fails safe (forces a look, never hides a real regression).
#
# Env:
#   SONAR_TOKEN            required (same token the scan uses).
#   SONAR_PROJECT          default volchanskyi_opengate
#   SONAR_BRANCH           default dev
#   SONAR_API              default https://sonarcloud.io
#   DUP_CEILING            per-file ceiling, default 3 (= the gate threshold).
#   DUP_BASE               git ref changed files are compared against, default HEAD.
#   DUP_SETTLE_RETRIES     settle polls before evaluating anyway, default 12.
#   DUP_SETTLE_SLEEP       seconds between settle polls, default 5.
#   DUP_CHANGED_OVERRIDE   test seam: newline-separated file list (skips git).
#   DUP_ANCHORS_OVERRIDE   test seam: newline-separated anchor list (skips git;
#                          set-but-empty means "no anchors").
#   DUP_DENSITY_OVERRIDE   test seam: "path=density" lines (skips the API).
#   CURL_BIN               curl binary (stubbed in tests).
#
# Exit codes: 0 = every changed source file is at or below the ceiling (or there
#                 are no changed source files);
#             1 = a changed source file exceeds the ceiling;
#             2 = prerequisite missing (no SONAR_TOKEN and no density override).
set -uo pipefail

SONAR_PROJECT="${SONAR_PROJECT:-volchanskyi_opengate}"
SONAR_BRANCH="${SONAR_BRANCH:-dev}"
SONAR_API="${SONAR_API:-https://sonarcloud.io}"
DUP_CEILING="${DUP_CEILING:-3}"
DUP_BASE="${DUP_BASE:-HEAD}"
DUP_SETTLE_RETRIES="${DUP_SETTLE_RETRIES:-12}"
DUP_SETTLE_SLEEP="${DUP_SETTLE_SLEEP:-5}"
CURL_BIN="${CURL_BIN:-curl}"

# sdup_above_ceiling <value> <ceiling> — exit 0 (true) when value > ceiling.
# Float-safe via awk so "7.4" > "3" compares numerically, not lexically.
sdup_above_ceiling() {
  awk -v v="$1" -v c="$2" 'BEGIN { exit !((v + 0) > (c + 0)) }'
}

# sdup_is_source <path> — exit 0 when path is a SonarCloud-analyzed production
# source file: under a sonar.sources root, a Rust/Go/TS extension, and neither a
# test nor generated file (mirrors sonar.exclusions / sonar.test.inclusions).
sdup_is_source() {
  local p="$1"
  case "$p" in
    server/internal/* | agent/crates/* | web/src/*) ;;
    *) return 1 ;;
  esac
  case "$p" in
    *_test.go | *.test.ts | *.test.tsx | *.spec.ts | *.spec.tsx) return 1 ;;
    *_gen.go | *.pb.go) return 1 ;;
    */tests/* | */testdata/* | */testutil/* | */testpg/*) return 1 ;;
  esac
  case "$p" in
    *.rs | *.go | *.ts | *.tsx) return 0 ;;
    *) return 1 ;;
  esac
}

# sdup_changed_files — print changed + untracked source files, one per line.
sdup_changed_files() {
  if [ -n "${DUP_CHANGED_OVERRIDE:-}" ]; then
    printf '%s\n' "$DUP_CHANGED_OVERRIDE"
    return 0
  fi
  {
    git diff --name-only "$DUP_BASE" 2>/dev/null
    git ls-files --others --exclude-standard 2>/dev/null
  } | sort -u | while IFS= read -r f; do
    [ -n "$f" ] && sdup_is_source "$f" && printf '%s\n' "$f"
  done
}

# sdup_existed_at_base <path> — exit 0 when the file was tracked at DUP_BASE, so
# it is guaranteed a measure once the freshly-uploaded analysis is indexed. Such
# files anchor the settle wait below (a brand-new file legitimately has none).
sdup_existed_at_base() {
  if [ -n "${DUP_ANCHORS_OVERRIDE+x}" ]; then # set, possibly empty
    [ -n "$DUP_ANCHORS_OVERRIDE" ] && printf '%s\n' "$DUP_ANCHORS_OVERRIDE" | grep -qxF "$1"
    return
  fi
  git cat-file -e "$DUP_BASE:$1" 2>/dev/null
}

# sdup_settled <files> — exit 0 when the uploaded analysis is queryable: either
# no anchor files exist to wait on, or at least one anchor already reports a
# measure. Returns non-zero while anchors exist but none report yet (still
# indexing), so `make sonar`'s just-uploaded result is not read as an empty pass.
sdup_settled() {
  local f found_anchor=1 # 1 = no anchor seen yet (shell-false)
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    sdup_existed_at_base "$f" || continue
    found_anchor=0
    [ -n "$(sdup_density "$f")" ] && return 0
  done <<<"$1"
  # No anchors → nothing to wait on (settled). Anchors but none reporting → still
  # indexing (not settled).
  [ "$found_anchor" -eq 1 ]
}

# sdup_density <path> — print the file's absolute duplicated_lines_density
# (empty when SonarCloud has no measure for it, e.g. a brand-new file the last
# analysis did not include).
sdup_density() {
  local path="$1"
  if [ -n "${DUP_DENSITY_OVERRIDE:-}" ]; then
    printf '%s\n' "$DUP_DENSITY_OVERRIDE" \
      | awk -F= -v p="$path" '$1 == p { print $2; found = 1 } END { exit !found }'
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/measures/component?component=$SONAR_PROJECT:$path&branch=$SONAR_BRANCH&metricKeys=duplicated_lines_density" \
    | jq -r '.component.measures[]? | select(.metric=="duplicated_lines_density") | .value // empty' 2>/dev/null
}

sdup_main() {
  if [ -z "${DUP_DENSITY_OVERRIDE:-}" ] && [ -z "${SONAR_TOKEN:-}" ]; then
    echo "✗ sonar-duplication-guard: SONAR_TOKEN unset (and no DUP_DENSITY_OVERRIDE)." >&2
    return 2
  fi

  local files offenders=() checked=0 f d i=0
  files="$(sdup_changed_files)"
  if [ -z "$files" ]; then
    echo "✓ sonar-duplication-guard: no changed source files — nothing to guard" >&2
    return 0
  fi

  # `make sonar` waits on the compute engine, but the measures REST endpoint can
  # lag a few seconds behind indexing. Reading it too early returns empty for
  # every file and the guard would pass blindly, so wait for the analysis to
  # become queryable before evaluating.
  until sdup_settled "$files"; do
    i=$((i + 1))
    if [ "$i" -gt "$DUP_SETTLE_RETRIES" ]; then
      echo "⚠ sonar-duplication-guard: analysis measures not queryable after ${DUP_SETTLE_RETRIES} polls; evaluating with what is available" >&2
      break
    fi
    sleep "$DUP_SETTLE_SLEEP"
  done

  while IFS= read -r f; do
    [ -n "$f" ] || continue
    d="$(sdup_density "$f")"
    [ -n "$d" ] || continue
    checked=$((checked + 1))
    if sdup_above_ceiling "$d" "$DUP_CEILING"; then
      offenders+=("$f ${d}%")
    fi
  done <<<"$files"

  if [ "${#offenders[@]}" -gt 0 ]; then
    {
      echo "✗ duplicated_lines_density exceeds ${DUP_CEILING}% in changed file(s):"
      printf '    %s\n' "${offenders[@]}"
      echo "  The SonarCloud gate fails new_duplicated_lines_density > ${DUP_CEILING}. Locally the"
      echo "  new-code count under-reports (uncommitted lines have no blame), so this checks each"
      echo "  changed file's absolute duplication instead. Fix: extract the shared code."
      echo "  Inspect: https://sonarcloud.io/component_measures?id=${SONAR_PROJECT}&branch=${SONAR_BRANCH}&metric=duplicated_lines_density&view=list"
    } >&2
    return 1
  fi
  echo "✓ sonar-duplication-guard: ${checked} changed source file(s) at or below ${DUP_CEILING}% duplication" >&2
  return 0
}

# Run only when executed directly; sourcing exposes the functions for unit tests.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  sdup_main
fi
