#!/usr/bin/env bash
# Binds each section of the CI/CD rules to the checks that hold it: every result a guard prints
# names a check, every section lists the checks holding it, and each list matches what is printed.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RULES=(.claude/rules/ci-cd-determinism.md .claude/rules/workflow-inputs.md)

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Prints read<TAB>sections<TAB>checks, and one finding line per mismatch with the guards.
cat >"$WORK/binding.py" <<'PY'
import os
import re
import subprocess
import sys

ROOT, RULE = sys.argv[1], sys.argv[2]
SELF = "scripts/tests/rule-check-binding.test.sh"
GUARD = re.compile(r"scripts/tests/[a-z0-9-]+\.test\.sh")
RESULT = re.compile(r"^  (ok  |FAIL) (.*)$")
NAMED = re.compile(r"^\[([a-z][a-z0-9-]*)\] ")
LISTED = re.compile(r"`([a-z][a-z0-9-]*)`")

text = open(os.path.join(ROOT, RULE), encoding="utf-8").read()
enforced = text.split("**Enforced by:**", 1)[1].split("\n\n", 1)[0] if "**Enforced by:**" in text else ""
guards = sorted(set(GUARD.findall(enforced)) - {SELF})
if not guards:
    print("finding\tthe rule names no guard under 'Enforced by'")

listed, section, in_rules = {}, None, False
for line in text.split("\n"):
    if line.startswith("## "):
        in_rules, section = line == "## The rules", None
    elif in_rules and line.startswith("### "):
        section = line[4:].strip()
        listed[section] = set()
    elif section and line.startswith("**Checks:**"):
        listed[section] |= set(LISTED.findall(line))

printed = set()
for guard in guards:
    run = subprocess.run([os.path.join(ROOT, guard)], capture_output=True, text=True, cwd=ROOT)
    for line in (run.stdout + run.stderr).split("\n"):
        result = RESULT.match(line)
        if not result:
            continue
        named = NAMED.match(result.group(2))
        if named:
            printed.add(named.group(1))
        else:
            print(f"finding\t{guard} prints a result naming no check: {result.group(2)[:80]}")

for name, checks in listed.items():
    if not checks:
        print(f"finding\tthe section '{name}' lists no check")
for check in sorted(set().union(*listed.values()) - printed):
    print(f"finding\tthe check '{check}' is listed and no guard prints it")
for check in sorted(printed - set().union(*listed.values())):
    print(f"finding\tthe check '{check}' is printed and no section lists it")
print(f"read\t{len(listed)}\t{len(printed)}")
PY

echo "rule-check-binding:"

DEMO="$WORK/demo"
mkdir -p "$DEMO/scripts/tests"
cat >"$DEMO/scripts/tests/held.test.sh" <<'SH'
#!/usr/bin/env bash
printf '  ok   [held] a check its section lists\n'
printf '  ok   [orphan] a check no section lists\n'
printf '  FAIL a result naming no check\n' >&2
SH
chmod +x "$DEMO/scripts/tests/held.test.sh"
cat >"$DEMO/rule.md" <<'MD'
# A rule

**Enforced by:**
[`scripts/tests/held.test.sh`](scripts/tests/held.test.sh). **No bypass.**

## The rules

### A section held by a check

**Checks:** `held`, `ghost`.

### A section held by nothing
MD
demo="$(python3 "$WORK/binding.py" "$DEMO" rule.md)"
expected="the section 'A section held by nothing' lists no check
the check 'ghost' is listed and no guard prints it
the check 'orphan' is printed and no section lists it
scripts/tests/held.test.sh prints a result naming no check: a result naming no check"
if [ "$(grep '^finding' <<<"$demo" | cut -f2- | sort)" = "$(sort <<<"$expected")" ]; then
  pass "demonstrated: an unlisted section, a listed check nobody prints, a printed check nobody lists and an unnamed result are each caught"
else
  fail "demonstrated: the binding does not catch each mismatch [$demo]"
fi

for rule in "${RULES[@]}"; do
  name="$(basename "$rule")"
  if [ ! -f "$REPO_ROOT/$rule" ]; then
    fail "$name exists"
    continue
  fi
  if grep -qF "rules/$name" "$REPO_ROOT/CLAUDE.md"; then
    pass "$name is indexed in CLAUDE.md"
  else
    fail "$name is not indexed in CLAUDE.md, so nothing points a reader at it"
  fi
  found="$(python3 "$WORK/binding.py" "$REPO_ROOT" "$rule")"
  read -r sections checks <<<"$(awk -F'\t' '$1 == "read" { print $2, $3 }' <<<"$found")"
  mismatches="$(awk -F'\t' '$1 == "finding" { print "      " $2 }' <<<"$found")"
  if [ "${sections:-0}" -eq 0 ] || [ "${checks:-0}" -eq 0 ]; then
    fail "$name: the binding read ${sections:-0} sections and ${checks:-0} printed checks, so it compared nothing"
  elif [ -z "$mismatches" ]; then
    pass "each of the $sections sections of $name lists the checks that hold it, $checks in all"
  else
    fail "$name and its guards disagree:"$'\n'"$mismatches"
  fi
done

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
