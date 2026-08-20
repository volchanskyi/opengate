#!/usr/bin/env bash
# Enforces the three-tree split of docs/: product (what the system does),
# architecture (how it is built), infrastructure (how it runs).
#
# The seam exists because docs organised by engineering concern describe every
# product capability inside a build-or-run chapter, which is how the same fact
# ends up written twice and drifting in one copy. Conventions in docs/README.md.
#
# Exempt from rule 1 by design: README.md and Home.md are the tree's own front
# matter, and Architecture-Decision-Records.md is the ADR corpus rather than a
# chapter — relocating it is a separate decision about ADR numbering.

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

# Every Markdown file under docs/ that is a chapter: excludes the ADR corpus,
# the generated API reference, and the tree's own front matter.
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

# --- Rule 1: every chapter lives in exactly one of the three trees.
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

# --- Rule 2: every chapter appears in exactly one Home.md index row.
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

# --- Rule 3: a product chapter never links a build-or-run path.
# A product chapter reaching for a deploy path means mechanism leaked back
# across the seam; the owning infrastructure chapter should carry it instead.
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
