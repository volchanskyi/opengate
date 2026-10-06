#!/usr/bin/env bash
# pretooluse-comment-check.sh — refuses a Write, Edit or MultiEdit that adds a comment
# violating .claude/rules/code-comments.md. NO BYPASS.
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

case "${HOOK_TOOL_INPUT_FILE_PATH:-}" in
  "" | *.md) exit 0 ;;
esac

source_dir="$PROJECT_ROOT/scripts/check-comments"
cache_dir="${TMPDIR:-/tmp}/opengate-comment-check"
source_key="$(
  find "$source_dir" -type f -name '*.go' -print0 \
    | sort -z \
    | xargs -0 cksum \
    | cksum \
    | awk '{print $1 "-" $2}'
)"
binary="$cache_dir/check-comments-$source_key"

if [ ! -x "$binary" ]; then
  mkdir -p "$cache_dir"
  temporary_binary="$binary.$$"
  (cd "$PROJECT_ROOT" && GO111MODULE=off go build -o "$temporary_binary" ./scripts/check-comments)
  chmod +x "$temporary_binary"
  mv "$temporary_binary" "$binary"
fi

status=0
output="$("$binary" --root "$PROJECT_ROOT" --typescript "$PROJECT_ROOT/web/node_modules/typescript" hook <<<"$HOOK_INPUT" 2>&1)" || status=$?
case "$status" in
  0) exit 0 ;;
  1)
    block code-comments "Write/Edit refused: the proposed comment breaks .claude/rules/code-comments.md:
$output
State what the code does or why, positively, in at most two lines."
    ;;
esac
block code-comments "Write/Edit refused: the comment check could not run:
$output"
