#!/usr/bin/env bash
# Shared hook helpers: input is JSON on stdin, allow is exit 0, block is exit 2 with a message.
# No environment variable bypasses a hook; enforcement changes only through .claude/settings.json.

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

# Each field is a dotted path with an optional :default, set as HOOK_<PATH_UPPERCASED>.
parse_input_fields() {
  read_hook_input
  local script
  script='
import json, sys
data = sys.stdin.read()
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
  exports="$(python3 -c "$script" "$@" <<<"$HOOK_INPUT")" || {
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
    >>"$log_dir/blocks.log" 2>/dev/null || true
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

# Any uncaught error becomes a block.
_fail_closed_handler() {
  local exit_code=$?
  case "$exit_code" in
    0 | 2) exit "$exit_code" ;;
  esac
  local hook_name
  hook_name="$(basename "${BASH_SOURCE[1]:-$0}")"
  printf 'hook %s: internal error (exit %s) — failing closed\n' "$hook_name" "$exit_code" >&2
  exit 2
}

enable_fail_closed_hook() {
  trap _fail_closed_handler ERR
}

# Pre-verb tokens are options with an optional value word, since `-c` takes a separate value token.
# Requiring an option lead keeps `git log --grep=…` from matching.
git_verb_re() {
  printf '\\bgit[[:space:]]+(-[^[:space:]]+[[:space:]]+([^-][^[:space:]]*[[:space:]]+)?)*%s\\b' "$1"
}
