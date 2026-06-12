#!/usr/bin/env bash
# pretooluse-doc-link-check.sh — validate proposed Markdown against repo links.
#
# Triggers on PreToolUse Write|Edit|MultiEdit. Only docs/** and .claude/**/*.md
# are in scope. The checker applies the proposed edit in memory, then validates
# the full scoped tree so heading edits cannot silently break inbound links.
#
# NO BYPASS.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

read_hook_input
parse_input_fields tool_name tool_input.file_path

case "${HOOK_TOOL_NAME:-}" in
  Write | Edit | MultiEdit) : ;;
  *) exit 0 ;;
esac

path="${HOOK_TOOL_INPUT_FILE_PATH:-}"
case "$path" in
  docs/*.md | */docs/*.md | .claude/*.md | */.claude/*.md) ;;
  *) exit 0 ;;
esac

source_dir="$PROJECT_ROOT/scripts/check-doc-links"
cache_dir="${TMPDIR:-/tmp}/opengate-doc-link-check"
source_key="$(
  find "$source_dir" -type f -name '*.go' -print0 \
    | sort -z \
    | xargs -0 cksum \
    | cksum \
    | awk '{print $1 "-" $2}'
)"
binary="$cache_dir/check-doc-links-$source_key"

if [ ! -x "$binary" ]; then
  mkdir -p "$cache_dir"
  temporary_binary="$binary.$$"
  (cd "$PROJECT_ROOT" && GO111MODULE=off go build -o "$temporary_binary" ./scripts/check-doc-links)
  chmod +x "$temporary_binary"
  mv "$temporary_binary" "$binary"
fi

if output="$("$binary" --root "$PROJECT_ROOT" --hook <<<"$HOOK_INPUT" 2>&1)"; then
  exit 0
fi

block doc-links "Write/Edit refused: proposed Markdown introduces or preserves invalid repository links:
$output
Fix the target/anchor or use an archived plan path. scripts/check-doc-links/."
