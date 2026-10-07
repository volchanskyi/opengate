#!/usr/bin/env bash
# Enforces the three-tree split of docs/: product, architecture, infrastructure.
# README.md, Home.md and Architecture-Decision-Records.md are exempt from rule 1.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HOME_INDEX="$REPO_ROOT/docs/Home.md"

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

TREES="product architecture infrastructure"

# Chapters exclude the decision records, the generated API reference and the front matter.
chapters() {
  find "$REPO_ROOT/docs" -type f -name '*.md' \
    -not -path "$REPO_ROOT/docs/adr/*" \
    -not -path "$REPO_ROOT/docs/api/*" \
    -not -path "$REPO_ROOT/docs/README.md" \
    -not -path "$REPO_ROOT/docs/Home.md" \
    -not -path "$REPO_ROOT/docs/Architecture-Decision-Records.md" \
    | sed "s|^$REPO_ROOT/||" | sort
}

echo "docs seam:"

stray=0
total=0
while IFS= read -r chapter; do
  [ -n "$chapter" ] || continue
  total=$((total + 1))
  tree="$(printf '%s\n' "$chapter" | cut -d/ -f2)"
  case " $TREES " in
    *" $tree "*) : ;;
    *)
      stray=$((stray + 1))
      fail "$chapter is not in one of docs/{product,architecture,infrastructure}/"
      ;;
  esac
done < <(chapters)

if [ "$total" -eq 0 ]; then
  fail "no chapters found under docs/ — the scope filter matched nothing"
elif [ "$stray" -eq 0 ]; then
  pass "all $total chapters live in one of the three trees"
fi

missing=0
duplicated=0
while IFS= read -r chapter; do
  [ -n "$chapter" ] || continue
  target="./${chapter#docs/}"
  rows="$(grep -cF "]($target)" "$HOME_INDEX" || true)"
  if [ "$rows" -eq 0 ]; then
    missing=$((missing + 1))
    fail "$chapter has no Home.md index row"
  elif [ "$rows" -gt 1 ]; then
    duplicated=$((duplicated + 1))
    fail "$chapter has $rows Home.md index rows; a chapter belongs to one tree"
  fi
done < <(chapters)

if [ "$missing" -eq 0 ] && [ "$duplicated" -eq 0 ] && [ "$total" -gt 0 ]; then
  pass "every chapter has exactly one Home.md index row"
fi

# A product chapter linking a build-or-run path leaks mechanism across the seam.
leaked=0
mechanism_links() {
  grep -noE '\]\([^)]*(deploy/|\.github/|Makefile|scripts/)[^)]*\)' "$1" || true
}
while IFS= read -r chapter; do
  [ -n "$chapter" ] || continue
  case "$chapter" in
    docs/product/*) : ;;
    *) continue ;;
  esac
  while IFS= read -r hit; do
    [ -n "$hit" ] || continue
    leaked=$((leaked + 1))
    fail "$chapter:${hit%%:*} links a build-or-run path: ${hit#*:}"
  done < <(mechanism_links "$REPO_ROOT/$chapter")
done < <(chapters)

if [ "$leaked" -eq 0 ]; then
  pass "no product chapter links deploy/, .github/, Makefile or scripts/"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
