#!/usr/bin/env bash
# Moves credential-bearing permission entries from settings.json to settings.local.json on commit.
# It runs before the commit guard so the gauntlet sees the cleaned file, and it never blocks.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"
[ -n "$cmd" ] || exit 0

if ! grep -qE "$(git_verb_re commit)" <<<"$cmd"; then
  exit 0
fi

repo="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
tracked="$repo/.claude/settings.json"
local_file="$repo/.claude/settings.local.json"

[ -f "$tracked" ] || exit 0

# Read into a variable because grep -q exits at its first match and pipefail would fail the writer.
staged_settings="$(git -C "$repo" diff --cached --name-only -- .claude/settings.json)"
was_staged=false
if [ -n "$staged_settings" ]; then
  was_staged=true
fi

moved=$(
  python3 - "$tracked" "$local_file" <<'PYEOF' || true
import json, re, sys

tracked_path, local_path = sys.argv[1], sys.argv[2]

SECRET_PATTERNS = [
    re.compile(r'Authorization:\s*Basic\s+[A-Za-z0-9+/]{16,}={0,2}', re.IGNORECASE),
    re.compile(r'Authorization:\s*Bearer\s+(?!\$\()[A-Za-z0-9._\-]{20,}', re.IGNORECASE),
    re.compile(r'\b(?:ghp|gho|ghu|ghs|ghr|glpat|sk-live|sk-test|sk-)_[A-Za-z0-9_]{20,}'),
    re.compile(r'\b(?:password|passwd)\s*[:=]\s*(?!\$\()[^\s\'"]{4,}', re.IGNORECASE),
    re.compile(r'\b(AKIA|ASIA)[A-Z0-9]{16}\b'),
]

try:
    with open(tracked_path) as f:
        tracked = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    print(0)
    sys.exit(0)

allow = tracked.get('permissions', {}).get('allow', [])
secrets, safe = [], []
for e in allow:
    if any(p.search(e) for p in SECRET_PATTERNS):
        secrets.append(e)
    else:
        safe.append(e)

if not secrets:
    print(0)
    sys.exit(0)

try:
    with open(local_path) as f:
        local = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    local = {}

local.setdefault('permissions', {}).setdefault('allow', [])
existing = set(local['permissions']['allow'])
for s in secrets:
    if s not in existing:
        local['permissions']['allow'].append(s)
        existing.add(s)

tracked.setdefault('permissions', {})['allow'] = safe

# Write local first; if that fails we don't truncate tracked.
with open(local_path, 'w') as f:
    json.dump(local, f, indent=2)
    f.write('\n')
with open(tracked_path, 'w') as f:
    json.dump(tracked, f, indent=2)
    f.write('\n')

print(len(secrets))
PYEOF
)

if [ "${moved:-0}" != "0" ] && [ "$was_staged" = "true" ]; then
  git -C "$repo" add -- .claude/settings.json
  printf '[settings-secrets-rebalancer] moved %s credential-bearing entries from settings.json to settings.local.json; re-staged settings.json\n' "$moved" >&2
elif [ "${moved:-0}" != "0" ]; then
  printf '[settings-secrets-rebalancer] moved %s credential-bearing entries from settings.json to settings.local.json (working tree only — file was not staged)\n' "$moved" >&2
fi

exit 0
