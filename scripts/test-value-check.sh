#!/usr/bin/env bash
# test-value-check.sh — the analyser behind .claude/rules/test-value.md.
#
# It refuses two shapes in a web test file, and only two. Both were found by
# measuring which breakages the suite notices, and neither fires on a test that
# is doing its job (.claude/rules/test-value.md states why the shape-based
# rules that look attractive were rejected).
#
#   1. A test that never binds the primary export of the module it is named
#      for. That is the shape of a test which copied production code into
#      itself and asserted on the copy: the assertions pass while the shipped
#      module goes unexercised.
#
#   2. A global or prototype reassignment with no restore. What such a test
#      reports depends on what ran before it, and it changes the answer for
#      every test that runs after.
#
# Usage:
#   test-value-check.sh file <path> [content-file]
#       Analyse one file. <path> is repository-relative and decides which
#       sibling module the file is named for; when <content-file> is given the
#       content is read from there instead of from <path>, so a hook can judge
#       the file an edit would produce.
#   test-value-check.sh sweep
#       Analyse every tracked web test file.
#
# Exit 0 when clean, 1 with one line per violation on stderr otherwise.
# TEST_VALUE_ROOT overrides the repository root (the behavioural tests point it
# at a fixture tree).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="${TEST_VALUE_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"

# Entries exempt from the primary-export check, each naming why. An entry is
# re-earned, not kept: the sweep fails on an entry whose file is now clean, so
# the exemption cannot outlive the defect it covers.
ALLOWLIST_PRIMARY_EXPORT=(
  # web/src/lib/api.test.ts — the defect the check was written for. It copies
  # the production auth middleware into the test and asserts on the copy, so
  # the shipped client is unexercised. Removed with the rewrite that binds the
  # real middleware.
  "web/src/lib/api.test.ts"
)

usage() {
  cat >&2 <<'EOF'
usage: test-value-check.sh file <path> [content-file]
       test-value-check.sh sweep
EOF
  exit 2
}

# The analyser proper. Written without a dollar sign anywhere so it can live in
# a single-quoted shell string; the identifier pattern spells its dollar as a
# hex escape for the same reason.
ANALYSE_PY='
import json, os, re, sys

root = sys.argv[1]
allow_primary = set(json.loads(sys.argv[2]))
# Remaining argv: repeated pairs of <repo-relative path> <content file or "-">.
pairs = sys.argv[3:]

TEST_RE = re.compile(r"\.(test|spec)\.(ts|tsx|js|jsx)\Z")
MODULE_EXT_RE = re.compile(r"\.(ts|tsx|js|jsx)\Z")
IMPORT_RE = re.compile(
    "import\\s+(?P<clause>[^;]*?)\\s+from\\s+[\x22\x27](?P<mod>[^\x22\x27]+)[\x22\x27]"
)
IDENT = "[A-Za-z_\\x24][\\w\\x24]*"
ASSIGN_RE = re.compile(
    "(?:^|[^\\w.\\x24])((?:global|globalThis|window|self)\\.%s|%s\\.prototype\\.%s)\\s*=(?!=)"
    % (IDENT, IDENT, IDENT)
)
RESTORE_OPENERS = re.compile("\\bfinally\\b|\\bafter(?:Each|All)\\s*\\(")


def camel(name):
    parts = re.split(r"[-_.]", name)
    return parts[0] + "".join(p[:1].upper() + p[1:] for p in parts[1:])


def read(path):
    with open(path, encoding="utf-8", errors="replace") as fh:
        return fh.read()


def sibling_module(rel):
    """The source module a test file is named for: its basename and its text."""
    base = TEST_RE.sub("", rel)
    for ext in (".ts", ".tsx", ".js", ".jsx"):
        cand = os.path.join(root, base + ext)
        if os.path.isfile(cand):
            return os.path.basename(base), read(cand)
    return None, None


def primary_export(mod_basename, mod_text):
    """The export carrying the module own name, or None when there is none."""
    name = camel(mod_basename)
    declared = re.compile(
        "export\\s+(?:default\\s+)?(?:const|let|var|class|function|async\\s+function)\\s+%s\\b"
        % re.escape(name)
    )
    if declared.search(mod_text):
        return name
    # An export-list re-export reaches the same place as a declaration.
    for block in re.findall(r"export\s*\{([^}]*)\}", mod_text):
        for part in block.split(","):
            part = re.sub(r"^type\s+", "", part.strip())
            if part and part.split(" as ")[-1].strip() == name:
                return name
    return None


def bindings_from(text, mod_basename):
    """Identifiers the file binds out of the sibling module."""
    bound = set()
    for m in IMPORT_RE.finditer(text):
        mod = m.group("mod")
        if not mod.startswith("."):
            continue
        if os.path.basename(MODULE_EXT_RE.sub("", mod)) != mod_basename:
            continue
        clause = m.group("clause")
        if "*" in clause:
            bound.add("*")
        for block in re.findall(r"\{([^}]*)\}", clause):
            for part in block.split(","):
                part = re.sub(r"^type\s+", "", part.strip())
                if part:
                    bound.add(part.split(" as ")[0].strip())
        head = re.sub(r"\{[^}]*\}", "", clause).strip().strip(",").strip()
        for h in head.split(","):
            h = h.strip()
            if h and re.fullmatch(IDENT, h):
                bound.add(h)
    return bound


def balanced_regions(text, opener_re):
    """Text of each brace-balanced block introduced by a matched opener."""
    out = []
    for m in opener_re.finditer(text):
        start = text.find("{", m.end() - 1)
        if start == -1:
            continue
        depth = 0
        for i in range(start, len(text)):
            if text[i] == "{":
                depth += 1
            elif text[i] == "}":
                depth -= 1
                if depth == 0:
                    out.append(text[start:i + 1])
                    break
    return out


def unrestored_targets(text):
    """Globals and prototype members assigned but never put back."""
    targets = [m.group(1) for m in ASSIGN_RE.finditer(text)]
    if not targets:
        return []
    restored = set()
    for region in balanced_regions(text, RESTORE_OPENERS):
        for m in ASSIGN_RE.finditer(region):
            restored.add(m.group(1))
    seen, out = set(), []
    for t in targets:
        if t not in restored and t not in seen:
            seen.add(t)
            out.append(t)
    return out


def unbound_primary(rel, text):
    """The primary export the test is named for but never binds, if any."""
    mod_basename, mod_text = sibling_module(rel)
    if mod_text is None:
        return None
    primary = primary_export(mod_basename, mod_text)
    if primary is None:
        return None
    bound = bindings_from(text, mod_basename)
    if "*" in bound or primary in bound:
        return None
    return (primary, mod_basename)


violations = []
exemptions_used = set()

for i in range(0, len(pairs), 2):
    rel, content_file = pairs[i], pairs[i + 1]
    if not TEST_RE.search(rel):
        continue
    src = content_file if content_file != "-" else os.path.join(root, rel)
    if not os.path.isfile(src):
        continue
    text = read(src)

    missing = unbound_primary(rel, text)
    if missing is not None:
        if rel in allow_primary:
            exemptions_used.add(rel)
        else:
            violations.append(
                "%s: does not bind %s, the primary export of ./%s that it is named "
                "for. A test that copies production code and asserts on the copy "
                "leaves the shipped module unexercised."
                % (rel, missing[0], missing[1])
            )

    for target in unrestored_targets(text):
        violations.append(
            "%s: reassigns %s with no restore. Put it behind a try/finally or an "
            "afterEach that puts the original back, so what this test reports does "
            "not depend on what ran before it." % (rel, target)
        )

# An exemption is re-earned. Only the sweep can tell a stale entry from a file
# it simply was not asked about, so only the sweep reports one.
if len(pairs) > 2:
    for rel in sorted(allow_primary - exemptions_used):
        violations.append(
            "%s: is exempt from the primary-export check but no longer needs the "
            "exemption. Delete its entry from ALLOWLIST_PRIMARY_EXPORT in "
            "scripts/test-value-check.sh." % rel
        )

for v in violations:
    sys.stderr.write("test-value: %s\n" % v)
sys.exit(1 if violations else 0)
'

allow_json="$(printf '%s\n' "${ALLOWLIST_PRIMARY_EXPORT[@]}" \
  | python3 -c 'import json,sys; print(json.dumps([l for l in sys.stdin.read().split(chr(10)) if l]))')"

case "${1:-}" in
  file)
    [ "$#" -ge 2 ] && [ "$#" -le 3 ] || usage
    rel="${2#"$ROOT/"}"
    rel="${rel#./}"
    python3 -c "$ANALYSE_PY" "$ROOT" "$allow_json" "$rel" "${3:--}"
    ;;
  sweep)
    [ "$#" -eq 1 ] || usage
    args=()
    while IFS= read -r -d '' rel; do
      case "$rel" in
        *.test.ts | *.test.tsx | *.spec.ts | *.spec.tsx) args+=("$rel" "-") ;;
        *) ;;
      esac
    done < <(git -C "$ROOT" ls-files -z -- web)
    [ "${#args[@]}" -gt 0 ] || {
      printf 'test-value: sweep found no web test files\n' >&2
      exit 1
    }
    python3 -c "$ANALYSE_PY" "$ROOT" "$allow_json" "${args[@]}"
    ;;
  *)
    usage
    ;;
esac
