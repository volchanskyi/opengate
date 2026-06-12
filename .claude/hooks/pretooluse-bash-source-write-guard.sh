#!/usr/bin/env bash
# pretooluse-bash-source-write-guard.sh — catch source-file writes via Bash.
#
# pretooluse-tdd-gate.sh covers Write/Edit/MultiEdit tool calls. Shell can
# also write files (`echo > foo`, `cat >>`, `sed -i`, `tee`). This hook
# scans the Bash command for those patterns targeting paths inside the
# repo, classifies each via scripts/tdd-check.sh is-source, and applies
# the same TDD gate.
#
# Best-effort regex. The commit-guard's TDD backup check (§2.4 rule 7 of
# the plan) is the final safety net for anything this misses.
#
# NO BYPASS.
set -euo pipefail
# shellcheck source=lib/common.sh
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"
[ -n "$cmd" ] || exit 0

# Extract candidate write-target paths from the command. Sent to Python for
# robust tokenization (handles quoting and operators that bash regex can't
# cleanly parse).
candidates=$(
  CMD="$cmd" python3 - <<'PYEOF'
import os, re, sys
cmd = os.environ.get("CMD", "")
paths = set()

# Shell redirection: > path, >> path. Match the next token after the operator.
for m in re.finditer(r'>>?\s*([^\s&|;<>()`"\']+)', cmd):
    paths.add(m.group(1))
# Quoted redirection targets (single or double quotes).
for m in re.finditer(r'>>?\s*"([^"]+)"', cmd):
    paths.add(m.group(1))
for m in re.finditer(r">>?\s*'([^']+)'", cmd):
    paths.add(m.group(1))

# sed -i ... <path> (in-place edit). sed flag like -i, -i'' or -i.bak, then
# script, then file(s).
for m in re.finditer(r'\bsed\b(?:\s+-[A-Za-z]*i[A-Za-z\.\']*\S*)\s+(.*)', cmd):
    rest = m.group(1)
    tokens = rest.split()
    # Skip the sed script (first token); subsequent tokens are file paths.
    for t in tokens[1:]:
        if t.startswith("-"):
            continue
        if t.startswith("'") and t.endswith("'"):
            t = t[1:-1]
        if t.startswith('"') and t.endswith('"'):
            t = t[1:-1]
        paths.add(t)
        # only the first non-flag file matters in most cases; keep all to be safe

# tee <path>, tee -a <path>, tee -i -a <path>...
for m in re.finditer(r'\btee\b((?:\s+-[A-Za-z]+)*)\s+(\S+)', cmd):
    p = m.group(2)
    if p.startswith("'") and p.endswith("'"):
        p = p[1:-1]
    if p.startswith('"') and p.endswith('"'):
        p = p[1:-1]
    paths.add(p)

for p in paths:
    print(p)
PYEOF
)

[ -n "$candidates" ] || exit 0

repo_root="$(project_root)"

# Check each candidate.
while IFS= read -r raw; do
  [ -n "$raw" ] || continue
  # Resolve absolute path relative to CWD (which the harness sets to the project dir).
  case "$raw" in
    /*) abs="$raw" ;;
    *) abs="$PWD/$raw" ;;
  esac
  # Canonicalize without requiring the file to exist.
  abs="$(python3 -c 'import os,sys; print(os.path.normpath(sys.argv[1]))' "$abs")"

  # Only consider paths inside the repo working tree.
  case "$abs" in
    "$repo_root"/*) : ;;
    *) continue ;;
  esac

  # Use the path relative to repo root for the classifier.
  rel="${abs#"$repo_root"/}"

  if ! is_source_path "$rel"; then
    continue
  fi

  if branch_has_test_change; then
    exit 0
  fi

  msg=$(
    cat <<EOF
TDD violation (Bash form). Per .claude/rules/tdd.md, the failing test MUST be written BEFORE the source code.
The Bash command would modify ${rel}, a source file, on a branch that has no test files modified, added, or staged. Stage a test first.
Detected command: ${cmd}
There is NO bypass.
EOF
  )
  block tdd-test-first "$msg"
done <<<"$candidates"

exit 0
