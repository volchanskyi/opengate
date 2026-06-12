#!/usr/bin/env bash
# pretooluse-git-commit-secrets-rebalancer.sh — enforce secret/non-secret split at commit time.
#
# Convention (enforced here, documented in CLAUDE.md / rules):
#   - .claude/settings.json       (tracked)    = hooks + safe permissions
#   - .claude/settings.local.json (gitignored) = credential-bearing permissions only
#
# Triggers on PreToolUse Bash. Filters for `git commit` verb. Before the
# commit-guard runs the gauntlet, this hook:
#   1. Scans .claude/settings.json for credential-bearing permission entries
#      (basic-auth headers, bearer tokens, GitHub/GitLab/Stripe/OpenAI token
#      prefixes, literal password=VALUE, AWS access key ids).
#   2. Moves any matches to .claude/settings.local.json (deduped).
#   3. If settings.json was previously staged, re-stages the rebalanced
#      content so the commit picks up the cleaned file.
#   4. Logs what moved to stderr.
#
# Never blocks the commit. Failures log to stderr and exit 0.
# Sequencing: wire this BEFORE pretooluse-git-commit-guard.sh in settings.json
# so the gauntlet sees the cleaned file.
#
# NO BYPASS.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"
[ -n "$cmd" ] || exit 0

# Filter: command must include a `git commit` verb (same pattern as commit-guard).
if ! printf '%s' "$cmd" | grep -qE '\bgit[[:space:]]+(-[^[:space:]]+[[:space:]]+)*commit\b'; then
  exit 0
fi

repo="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
tracked="$repo/.claude/settings.json"
local_file="$repo/.claude/settings.local.json"

[ -f "$tracked" ] || exit 0

# Was settings.json already staged? (Need to re-stage after rebalance.)
was_staged=false
if git -C "$repo" diff --cached --name-only -- .claude/settings.json | grep -q .; then
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

# Re-stage if we touched a previously-staged file.
if [ "${moved:-0}" != "0" ] && [ "$was_staged" = "true" ]; then
  git -C "$repo" add -- .claude/settings.json
  printf '[settings-secrets-rebalancer] moved %s credential-bearing entries from settings.json to settings.local.json; re-staged settings.json\n' "$moved" >&2
elif [ "${moved:-0}" != "0" ]; then
  printf '[settings-secrets-rebalancer] moved %s credential-bearing entries from settings.json to settings.local.json (working tree only — file was not staged)\n' "$moved" >&2
fi

exit 0
