#!/usr/bin/env bash
# pretooluse-write-guard.sh — block writes/edits to forbidden paths/content.
#
# Triggers on PreToolUse Write|Edit|MultiEdit. Enforces:
#   1. No writes to ~/.claude/plans/ (use project .claude/plans/ instead).
#   2. ADRs in docs/adr/ (013+) are mutable, but may only link ARCHIVED plans.
#      A link to an active plan rots when the plan is archived/renamed; archived
#      plans are stable targets. Applies to Write/Edit/MultiEdit on any ADR.
#   3. No content additions matching NOSONAR, //nolint, nolint:,
#      sonar.issue.ignore.multicriteria, or eslint-disable*.
#
# NO BYPASS.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.file_path tool_input.content tool_input.new_string tool_input.edits

tool="${HOOK_TOOL_NAME:-}"
case "$tool" in
  Write | Edit | MultiEdit) : ;;
  *) exit 0 ;;
esac

path="${HOOK_TOOL_INPUT_FILE_PATH:-}"
[ -n "$path" ] || exit 0

# 1. ~/.claude/plans/ writes.
case "$path" in
  /home/ivan/.claude/plans/* | "$HOME/.claude/plans/"* | ~/.claude/plans/*)
    block plans-wrong-dir "Write/Edit refused: $path is under the user-global ~/.claude/plans/. Plans must live in /home/ivan/opengate/.claude/plans/. .claude/rules/plans-and-adrs.md."
    ;;
esac

# Determine the new content being written, across tool variants.
new_content=""
case "$tool" in
  Write) new_content="${HOOK_TOOL_INPUT_CONTENT:-}" ;;
  Edit) new_content="${HOOK_TOOL_INPUT_NEW_STRING:-}" ;;
  MultiEdit) new_content="${HOOK_TOOL_INPUT_EDITS:-}" ;;
esac

# 2. An ADR may not link a plan. A plan is a working document, deleted in the
# commit that lands its work, so a decision record that points at one rots.
# Fold the rationale inline — the ADR is the durable record.
if grep -qE '(^|/)docs/adr/ADR-[0-9]+.*\.md$' <<<"$path"; then
  if grep -qE '\]\([^)]*plans/[^)]*\.md' <<<"$new_content"; then
    block adr-plan-link "Write/Edit refused: $path links a plan file ( ](…plans/….md) ). A plan is deleted when its work lands, so an ADR linking one rots. Fold the rationale inline. .claude/rules/plans-and-adrs.md."
  fi
fi

# 3. Suppression patterns in new content.
if [ -n "$new_content" ]; then
  while IFS= read -r -d '' pattern_pair; do
    pattern="${pattern_pair%%|*}"
    label="${pattern_pair#*|}"
    if grep -qE "$pattern" <<<"$new_content"; then
      block sonar-suppress "Write/Edit refused: introduces ${label} in $path. .claude/rules/sonarcloud.md: no suppression without approval. Restructure the code so the linter is satisfied."
    fi
  done < <(printf '%s\0%s\0%s\0%s\0%s\0' \
    'NOSONAR|NOSONAR comment' \
    '//[[:space:]]*nolint|//nolint directive' \
    '#[[:space:]]*nolint:|#nolint: directive' \
    'sonar\.issue\.ignore\.multicriteria|sonar.issue.ignore.multicriteria entry' \
    'eslint-disable|eslint-disable directive')
fi

exit 0
