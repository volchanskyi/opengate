#!/usr/bin/env bash
# Enforces that documentation describes live state only.
#
# .claude/rules/docs-live-state.md forbids narrating what was removed, renamed
# or replaced. Nothing enforced it, and it was re-violated repeatedly, so this
# gate turns the rule into a gauntlet failure.
#
# Scope: docs/** minus docs/adr/** and docs/Architecture-Decision-Records.md. An
# ADR's Context section is required to state the problem the decision solved, so
# past-state is structural there and docs/README.md forbids deleting substantive
# ADR rationale. This is the same document-class boundary check-doc-links draws
# around .claude/plans/**: a scope definition, not an allowlist.
#
# Matching is paragraph-joined. Markdown wraps at ~80 columns, so a line-based
# grep misses every phrase that straddles a wrap ("has been / removed").
#
# There is no allowlist. The phrase list is deliberately narrower than the rule's
# prose: `used to `, `the old ` and `the previous ` match ordinary live writing
# ("used to construct the endpoint", "the previous successful run"), and a gate
# that fires on those is a gate somebody adds an allowlist to.

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

# In scope: every Markdown chapter under docs/, minus the ADR corpus.
in_scope_docs() {
  find "$REPO_ROOT/docs" -type f -name '*.md' \
    -not -path "$REPO_ROOT/docs/adr/*" \
    -not -path "$REPO_ROOT/docs/Architecture-Decision-Records.md" \
    | sort
}

# Collapse each blank-line-separated paragraph onto one line, prefixed by the
# line number the paragraph started on, so a phrase split across a wrap is still
# matched and still reports a usable location.
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
