#!/usr/bin/env bash
# Refuses a write to the user-global plans directory, a plan link in a decision record, a lint or
# Sonar suppression, and a write into .claude/.markers/.
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

case "$path" in
  /home/ivan/.claude/plans/* | "$HOME/.claude/plans/"* | ~/.claude/plans/*)
    block plans-wrong-dir "Write/Edit refused: $path is under the user-global ~/.claude/plans/. Plans must live in /home/ivan/opengate/.claude/plans/. .claude/rules/plans-and-adrs.md."
    ;;
esac

new_content=""
case "$tool" in
  Write) new_content="${HOOK_TOOL_INPUT_CONTENT:-}" ;;
  Edit) new_content="${HOOK_TOOL_INPUT_NEW_STRING:-}" ;;
  MultiEdit) new_content="${HOOK_TOOL_INPUT_EDITS:-}" ;;
esac

# A plan is deleted when its work lands, so a decision record linking one rots.
if grep -qE '(^|/)docs/adr/ADR-[0-9]+.*\.md$' <<<"$path"; then
  if grep -qE '\]\([^)]*plans/[^)]*\.md' <<<"$new_content"; then
    block adr-plan-link "Write/Edit refused: $path links a plan file ( ](…plans/….md) ). A plan is deleted when its work lands, so an ADR linking one rots. Fold the rationale inline. .claude/rules/plans-and-adrs.md."
  fi
fi

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

# Each marker is written by the step it proves, so a hand-written one proves nothing.
case "$path" in
  .claude/.markers/* | */.claude/.markers/*)
    block markers-direct-write "Write/Edit refused: $path is a marker. It is written by the step it proves — ./scripts/precommit-gauntlet.sh on a pass, scripts/refactor-gate.sh start/finish, the post-commit hook — never by hand. .claude/rules/refactor.md."
    ;;
esac

exit 0
