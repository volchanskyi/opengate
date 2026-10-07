#!/usr/bin/env bash
# Applies the TDD gate to shell writes of source files, and refuses any shell write into
# .claude/.markers/, whose files only the step each one proves writes.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"
[ -n "$cmd" ] || exit 0

# Python tokenizes the command, handling the quoting and operators a bash regex cannot.
candidates=$(
  CMD="$cmd" python3 - <<'PYEOF'
import os, re, shlex, sys
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
    print("W\t" + p)

# Writes that matter only for the markers: the other verbs that create, replace
# or remove a file, and an interpreter one-liner. A marker named anywhere in such
# a command is a target.
FILE_VERBS = {"cp", "mv", "ln", "install", "touch", "rm", "truncate", "rsync", "dd", "unlink"}
WRAPPERS = {"sudo", "command", "exec", "env", "xargs", "nohup", "time"}
for seg in re.split(r"\|\||&&|[;|&\n]", cmd):
    try:
        toks = shlex.split(seg)
    except ValueError:
        toks = seg.split()
    while toks and (toks[0] in WRAPPERS or re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", toks[0])):
        toks = toks[1:]
    if not toks or os.path.basename(toks[0]) not in FILE_VERBS:
        continue
    for t in toks[1:]:
        if t.startswith("of="):
            t = t[3:]
        if ".claude/.markers" in t:
            print("M\t" + t)
if re.search(r"\b(python3?|perl|node|ruby)\b", cmd) and ".claude/.markers" in cmd:
    print("M\t.claude/.markers")
PYEOF
)

[ -n "$candidates" ] || exit 0

repo_root="$(project_root)"

# Marker writes are refused before any source check.
while IFS=$'\t' read -r _ raw; do
  case "$raw" in
    *.claude/.markers*)
      block markers-direct-write "Bash write refused: the command writes ${raw}, a marker. It is written by the step it proves — ./scripts/precommit-gauntlet.sh on a pass, scripts/refactor-gate.sh start/finish, the post-commit hook — never by hand. .claude/rules/refactor.md.
Detected command: ${cmd}"
      ;;
  esac
done <<<"$candidates"

while IFS=$'\t' read -r kind raw; do
  [ "$kind" = "W" ] || continue
  [ -n "$raw" ] || continue
  case "$raw" in
    /*) abs="$raw" ;;
    *) abs="$PWD/$raw" ;;
  esac
  abs="$(python3 -c 'import os,sys; print(os.path.normpath(sys.argv[1]))' "$abs")"

  case "$abs" in
    "$repo_root"/*) : ;;
    *) continue ;;
  esac

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
