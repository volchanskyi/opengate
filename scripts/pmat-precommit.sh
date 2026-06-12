#!/usr/bin/env bash
# pmat-precommit.sh — ADR-019 integration point 2: the precommit TDG gate.
#
# Fails the commit if any CHANGED code file grades below B+ (PMAT TDG). This
# is the "B+ from day one, Clean-as-You-Code" gate: a file already below B+
# (e.g. server/internal/cert/cert.go = C) only blocks a commit once you touch
# it, at which point it must be lifted to the floor.
#
# Scope of "changed code": all changed code files INCLUDING tests, excluding
# machine-generated output (classified by scripts/tdd-check.sh `is-code`) and
# Go *test* files whose only change is gofmt formatting (ADR-019). Rationale:
# gofmt churn is not "changed code", and pmat itself skips test files in its
# analysis (`pmat tdg --explain` prints "Skipping test file"). Source files are
# never exempted — production quality is enforced on every touch, even a fmt-only
# one. Changed = union of (BASELINE_REF..HEAD) + staged + unstaged + untracked,
# matching the TDD gate's change detection.
#
# === Why not the literal ADR-019 command? ===
# ADR-019 §"Integration point 2" prescribes:
#     pmat tdg --since-commit HEAD~1 --threshold B+
# Neither flag exists in the pinned pmat@3.17.0: `--since-commit` is absent and
# `--threshold` is a *complexity* knob for --explain mode, not a grade floor.
# The grade floor in 3.17.0 is `check-quality --min-grade <GRADE> \
# --fail-on-violation`, and "changed files only" is computed here in git rather
# than by pmat. The DECISION (gate changed code at a B+ floor, no suppressions)
# is unchanged; only the mechanics differ. ADR-019 records the full mapping.
#
# Invoked by scripts/precommit-gauntlet.sh. Source-able for unit testing
# (scripts/tests/pmat-precommit.test.sh) — every step is a function and the
# pmat binary, version pin, grade floor, and diff baseline are all injectable.
#
# Env:
#   PMAT_BIN           pmat binary (default: pmat). Stubbed in tests.
#   PMAT_MIN_GRADE     grade floor (default: B+).
#   PMAT_BASELINE_REF  diff baseline ref (default: origin/dev).
#   PMAT_PIN           required pmat version (default: 3.17.0). Set empty to
#                      disable the version check (tests do this).
#   GOFMT_BIN          gofmt binary (default: gofmt) used by the ADR-019 fmt-only
#                      test-file exclusion. Stubbed in tests.
#
# Exit codes: 0 = all changed code meets the floor (or none changed);
#             1 = at least one changed file is below the floor;
#             2 = prerequisite missing (wrong/absent pmat).
set -uo pipefail

PMAT_BIN="${PMAT_BIN:-pmat}"
PMAT_MIN_GRADE="${PMAT_MIN_GRADE:-B+}"
PMAT_BASELINE_REF="${PMAT_BASELINE_REF:-origin/dev}"
# ${VAR-default}: an explicitly-empty PMAT_PIN disables the check; unset → pin.
PMAT_PIN="${PMAT_PIN-3.17.0}"
GOFMT_BIN="${GOFMT_BIN:-gofmt}"

PMAT_PRECOMMIT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TDD_CHECK="$PMAT_PRECOMMIT_DIR/tdd-check.sh"

# pmat_version_ok — true if PMAT_BIN reports exactly the pinned version, or if
# the pin is disabled. ADR-019 §5.5 pins pmat exactly (no patch auto-update).
pmat_version_ok() {
  [ -z "$PMAT_PIN" ] && return 0
  command -v "$PMAT_BIN" >/dev/null 2>&1 || return 1
  [ "$("$PMAT_BIN" --version 2>/dev/null)" = "pmat $PMAT_PIN" ]
}

# pmat_resolve_base — merge-base of HEAD with the first available baseline ref.
# Falls back to the repo root commit so a fresh repo without remotes still works.
pmat_resolve_base() {
  local ref
  for ref in "$PMAT_BASELINE_REF" origin/dev dev origin/main main; do
    if git rev-parse --verify --quiet "$ref" >/dev/null 2>&1; then
      git merge-base HEAD "$ref" 2>/dev/null && return 0
    fi
  done
  git rev-list --max-parents=0 HEAD 2>/dev/null | head -1
}

# pmat_is_gofmt_only_test <file> <base> — true iff <file> is a Go *test* file
# whose only change versus <base> is gofmt formatting (ADR-019). Such files are
# dropped from the TDG changed-set. The check is symmetric — gofmt(baseline) ==
# gofmt(current) — so any real edit (which changes the formatted form) keeps the
# file graded. Fail-safe: a non-test file, a missing baseline blob (new file), or
# an unavailable gofmt all return non-zero, so the file is graded rather than
# silently skipped.
pmat_is_gofmt_only_test() {
  local f="$1" base="$2"
  case "$f" in *_test.go) ;; *) return 1 ;; esac
  [ -n "$base" ] || return 1
  command -v "$GOFMT_BIN" >/dev/null 2>&1 || return 1
  git cat-file -e "$base:$f" 2>/dev/null || return 1
  local baseline_fmt current_fmt
  baseline_fmt="$(git show "$base:$f" 2>/dev/null | "$GOFMT_BIN" 2>/dev/null)" || return 1
  current_fmt="$("$GOFMT_BIN" "$f" 2>/dev/null)" || return 1
  [ "$baseline_fmt" = "$current_fmt" ]
}

# pmat_changed_code_files — print changed code files (one per line), filtered
# through tdd-check.sh is-code, restricted to files that still exist (a deleted
# file cannot be graded), and minus gofmt-only Go test files (ADR-019).
pmat_changed_code_files() {
  local base
  base="$(pmat_resolve_base 2>/dev/null || true)"
  {
    [ -n "$base" ] && git diff --name-only "$base"..HEAD 2>/dev/null
    git diff --cached --name-only 2>/dev/null
    git diff --name-only 2>/dev/null
    git ls-files --others --exclude-standard 2>/dev/null
  } | sort -u | while IFS= read -r f; do
    [ -n "$f" ] || continue
    [ -f "$f" ] || continue
    "$TDD_CHECK" is-code "$f" || continue
    pmat_is_gofmt_only_test "$f" "$base" && continue
    printf '%s\n' "$f"
  done
}

# pmat_check_file <file> — exit 0 if <file> meets the grade floor, 1 otherwise.
# On failure, prints the offending grade(s) to stderr.
pmat_check_file() {
  local f="$1" out rc
  out="$("$PMAT_BIN" tdg check-quality -p "$f" \
    --min-grade "$PMAT_MIN_GRADE" --fail-on-violation --format json 2>/dev/null)"
  rc=$?
  [ "$rc" -eq 0 ] && return 0
  # check-quality prints a progress banner before the JSON object; slice from
  # the first '{'. Best-effort pretty grade line; never let jq failure mask it.
  printf '%s' "$out" | sed -n '/^{/,$p' \
    | jq -r '.violations[]? | "    \(.path): grade \(.new_grade) (\(((.new_score // 0)*10|floor)/10))"' 2>/dev/null \
    || printf '    %s: below %s\n' "$f" "$PMAT_MIN_GRADE"
  return 1
}

pmat_precommit_main() {
  if ! pmat_version_ok; then
    {
      echo "✗ pmat $PMAT_PIN is required for the ADR-019 TDG gate (found: $("$PMAT_BIN" --version 2>/dev/null || echo 'not installed'))."
      echo "  Install the pinned version:  cargo install --locked --version $PMAT_PIN pmat"
    } >&2
    return 2
  fi

  local files
  files="$(pmat_changed_code_files)"
  if [ -z "$files" ]; then
    echo "✓ PMAT TDG gate: no changed code files to grade" >&2
    return 0
  fi

  local fail=0 count=0
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    count=$((count + 1))
    pmat_check_file "$f" || fail=1
  done <<<"$files"

  if [ "$fail" -ne 0 ]; then
    {
      echo "✗ PMAT TDG gate: changed code below $PMAT_MIN_GRADE (ADR-019)."
      echo "  Raise the grade (refactor) or record an ADR exception in the PR description."
      echo "  Inspect a file:  pmat tdg <file> --explain"
    } >&2
    return 1
  fi
  echo "✓ PMAT TDG gate: all $count changed code file(s) meet $PMAT_MIN_GRADE" >&2
  return 0
}

# Only run when executed directly; `source` for unit testing exposes the
# functions without running the gate.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  pmat_precommit_main
fi
