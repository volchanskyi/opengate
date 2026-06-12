#!/usr/bin/env bash
# Validate changed Shell files immediately after agent write operations.

set -euo pipefail

# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.file_path

case "${HOOK_TOOL_NAME:-}" in
  Write | Edit | MultiEdit) : ;;
  *) exit 0 ;;
esac

case "${HOOK_TOOL_INPUT_FILE_PATH:-}" in
  *.sh) ;;
  *) exit 0 ;;
esac

runner="$PROJECT_ROOT/scripts/shell-quality.sh"
if output="$("$runner" changed HEAD 2>&1)"; then
  exit 0
fi

block shell-quality "Shell edit failed syntax, lint, format, or execution-policy validation:
$output
Run scripts/shell-quality.sh changed HEAD after correcting the file."
