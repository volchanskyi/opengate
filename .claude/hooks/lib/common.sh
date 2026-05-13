#!/usr/bin/env bash
# Shared library for Claude Code hooks.
# Source from each hook: source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
#
# Provides:
#   read_hook_input             # cache stdin JSON into HOOK_INPUT (idempotent)
#   parse_input_fields PATH...  # extract dotted-path fields into HOOK_<NAME> vars
#   project_root                # echo the repo top-level (falls back to $PWD)
#   is_source_path PATH         # 0/1 via scripts/tdd-check.sh is-source
#   branch_has_test_change      # 0/1 via scripts/tdd-check.sh has-test-change
#   block RULE MESSAGE          # log to blocks.log, print MESSAGE to stderr, exit 2
#   warn RULE MESSAGE           # log to blocks.log with warn tag, print to stderr, exit 0
#
# Hook contract:
#   - Input: JSON on stdin from the Claude Code harness.
#   - Allow: exit 0 with no stdout (or hookSpecificOutput JSON for context injection).
#   - Block: exit 2 with a one-line stderr message (re-shown to Claude).
#   - Fail-closed on internal error: any uncaught error → exit 2.
#
# NO BYPASS. Hooks never honor environment variables like OPENGATE_HOOK_BYPASS.
# To change enforcement, edit .claude/settings.json.

set -euo pipefail

# Project-root-relative paths.
_COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_HOOKS_DIR="$(cd "$_COMMON_DIR/.." && pwd)"
PROJECT_ROOT="$(cd "$_HOOKS_DIR/../.." && pwd)"
TDD_CHECK="$PROJECT_ROOT/scripts/tdd-check.sh"

HOOK_INPUT=""
read_hook_input() {
  if [ -z "$HOOK_INPUT" ]; then
    HOOK_INPUT="$(cat || true)"
  fi
}

# parse_input_fields field1[:default1] field2[:default2] ...
# Field names are dotted (e.g. "tool_input.command"). Exports HOOK_<UPPER_WITH_UNDERSCORES>.
parse_input_fields() {
  read_hook_input
  local script
  script='
import json, os, sys
data = os.environ.get("HOOK_INPUT", "")
try:
    obj = json.loads(data) if data.strip() else {}
except Exception:
    obj = {}
for spec in sys.argv[1:]:
    if ":" in spec:
        path, default = spec.split(":", 1)
    else:
        path, default = spec, ""
    val = obj
    for p in path.split("."):
        if isinstance(val, dict) and p in val:
            val = val[p]
        else:
            val = default
            break
    if val is None:
        val = ""
    if isinstance(val, (dict, list)):
        val = json.dumps(val)
    name = "HOOK_" + path.upper().replace(".", "_")
    escaped = str(val).replace(chr(39), chr(39) + "\\" + chr(39) + chr(39))
    print(name + "=" + chr(39) + escaped + chr(39))
'
  local exports
  exports="$(HOOK_INPUT="$HOOK_INPUT" python3 -c "$script" "$@")" || {
    printf 'hook: failed to parse stdin JSON\n' >&2
    exit 2
  }
  eval "$exports"
}

project_root() {
  git rev-parse --show-toplevel 2>/dev/null || echo "$PWD"
}

is_source_path() {
  "$TDD_CHECK" is-source "$1"
}

branch_has_test_change() {
  "$TDD_CHECK" has-test-change
}

_log_event() {
  local tag="$1" rule="$2" msg="$3"
  local session_id="${CLAUDE_SESSION_ID:-unknown}"
  local uid="${UID:-$(id -u)}"
  local log_dir="${TMPDIR:-/tmp}/claude-${uid}/${session_id}"
  mkdir -p "$log_dir" 2>/dev/null || true
  local hook_name
  hook_name="$(basename "${BASH_SOURCE[2]:-${BASH_SOURCE[1]:-$0}}")"
  local summary
  summary="$(printf '%s' "$msg" | head -1 | tr '\t' ' ' | cut -c1-200)"
  printf '%s\t%s\t%s\t%s\t%s\n' "$(date -Is 2>/dev/null || date)" "$tag" "$rule" "$hook_name" "$summary" \
    >> "$log_dir/blocks.log" 2>/dev/null || true
}

block() {
  local rule="$1" msg="$2"
  _log_event BLOCK "$rule" "$msg"
  printf '%s\n' "$msg" >&2
  exit 2
}

warn() {
  local rule="$1" msg="$2"
  _log_event WARN "$rule" "$msg"
  printf '%s\n' "$msg" >&2
}

# Fail-closed: any uncaught error or signal becomes a block.
_fail_closed_handler() {
  local exit_code=$?
  # If we already exited cleanly (0 or 2), respect that.
  case "$exit_code" in
    0|2) exit "$exit_code" ;;
  esac
  local hook_name
  hook_name="$(basename "${BASH_SOURCE[1]:-$0}")"
  printf 'hook %s: internal error (exit %s) — failing closed\n' "$hook_name" "$exit_code" >&2
  exit 2
}
trap _fail_closed_handler ERR
