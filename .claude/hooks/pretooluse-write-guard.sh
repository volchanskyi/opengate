#!/usr/bin/env bash
# pretooluse-write-guard.sh — block writes/edits to forbidden paths/content.
#
# Triggers on PreToolUse Write|Edit|MultiEdit. Enforces:
#   1. No writes to ~/.claude/plans/ (use project .claude/plans/ instead).
#   2. ADRs in docs/adr/ are immutable — block Edit/MultiEdit on existing,
#      and block Write to an existing ADR. New ADR files are allowed.
#   3. No content additions matching NOSONAR, //nolint, nolint:,
#      sonar.issue.ignore.multicriteria, or eslint-disable*.
#
# NO BYPASS.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

parse_input_fields tool_name tool_input.file_path tool_input.content tool_input.new_string tool_input.edits

tool="${HOOK_TOOL_NAME:-}"
case "$tool" in
  Write|Edit|MultiEdit) : ;;
  *) exit 0 ;;
esac

path="${HOOK_TOOL_INPUT_FILE_PATH:-}"
[ -n "$path" ] || exit 0

# 1. ~/.claude/plans/ writes.
case "$path" in
  /home/ivan/.claude/plans/*|"$HOME/.claude/plans/"*|~/.claude/plans/*)
    block plans-wrong-dir "Write/Edit refused: $path is under the user-global ~/.claude/plans/. Plans must live in /home/ivan/opengate/.claude/plans/. CLAUDE.md §Project State."
    ;;
esac

# 2. ADR immutability.
if printf '%s' "$path" | grep -qE '(^|/)docs/adr/ADR-[0-9]+.*\.md$'; then
  if [ "$tool" = "Edit" ] || [ "$tool" = "MultiEdit" ]; then
    block adr-immutable "Edit refused: $path is an ADR. ADRs are immutable — supersede with a new file. CLAUDE.md §Project State."
  fi
  if [ "$tool" = "Write" ] && [ -e "$path" ]; then
    block adr-immutable "Write refused: $path is an existing ADR. ADRs are immutable — supersede with a new ADR file. CLAUDE.md §Project State."
  fi
fi

# 3. Suppression patterns in new content.
new_content=""
case "$tool" in
  Write)     new_content="${HOOK_TOOL_INPUT_CONTENT:-}" ;;
  Edit)      new_content="${HOOK_TOOL_INPUT_NEW_STRING:-}" ;;
  MultiEdit) new_content="${HOOK_TOOL_INPUT_EDITS:-}" ;;
esac

if [ -n "$new_content" ]; then
  while IFS= read -r -d '' pattern_pair; do
    pattern="${pattern_pair%%|*}"
    label="${pattern_pair#*|}"
    if printf '%s' "$new_content" | grep -qE "$pattern"; then
      block sonar-suppress "Write/Edit refused: introduces ${label} in $path. CLAUDE.md §SonarCloud §No suppression without approval. Restructure the code so the linter is satisfied."
    fi
  done < <(printf '%s\0%s\0%s\0%s\0%s\0' \
    'NOSONAR|NOSONAR comment' \
    '//[[:space:]]*nolint|//nolint directive' \
    '#[[:space:]]*nolint:|#nolint: directive' \
    'sonar\.issue\.ignore\.multicriteria|sonar.issue.ignore.multicriteria entry' \
    'eslint-disable|eslint-disable directive')
fi

exit 0
