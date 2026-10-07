#!/usr/bin/env bash
# Enforces .claude/rules/docs-live-state.md over docs/** minus the decision records.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

PASS=0
FAIL=0
FAILURES=()

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

BANNED='is deprecated|was deprecated|has been removed|(was|were) removed|previously|formerly|legacy|historically|kept for rollback|dormant'

in_scope_docs() {
  find "$REPO_ROOT/docs" -type f -name '*.md' \
    -not -path "$REPO_ROOT/docs/adr/*" \
    -not -path "$REPO_ROOT/docs/Architecture-Decision-Records.md" \
    | sort
}

# Each paragraph becomes one line prefixed by its start line, so a phrase split by a wrap matches.
paragraph_hits() {
  local file="$1"
  awk -v banned="$BANNED" '
    function flush() {
      if (buf != "" && tolower(buf) ~ banned) {
        printf "%d:%s\n", start, buf
      }
      buf = ""
      start = 0
    }
    /^[[:space:]]*$/ { flush(); next }
    {
      if (buf == "") { start = NR; buf = $0 }
      else { buf = buf " " $0 }
    }
    END { flush() }
  ' "$file"
}

echo "docs live state:"

scoped=0
violations=0
while IFS= read -r file; do
  scoped=$((scoped + 1))
  rel="${file#"$REPO_ROOT/"}"
  while IFS= read -r hit; do
    [ -n "$hit" ] || continue
    violations=$((violations + 1))
    line="${hit%%:*}"
    text="${hit#*:}"
    fail "$rel:$line describes past state: ${text:0:120}"
  done < <(paragraph_hits "$file")
done < <(in_scope_docs)

if [ "$scoped" -eq 0 ]; then
  fail "no docs were scanned — the scope filter matched nothing"
elif [ "$violations" -eq 0 ]; then
  pass "$scoped docs describe live state only"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
