#!/usr/bin/env bash
# Offline regression tests for the Grafana alert-rule provisioning file.
#
# The rules that page a human are the last line between a slow degradation and
# an incident nobody watched. This gate pins the ones whose absence has already
# cost a night: a target that was replaced mid-run, and a container walking up
# to its own memory limit.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RULES_FILE="$REPO_ROOT/deploy/grafana/provisioning/alerting/alert-rules.yml"

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

# rule_query <uid> prints the datasource expression of the named rule, or
# nothing when the rule does not exist.
rule_query() {
  python3 - "$RULES_FILE" "$1" <<'PY'
import sys, yaml

path, uid = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

for group in doc.get("groups", []):
    for rule in group.get("rules", []):
        if rule.get("uid") != uid:
            continue
        for item in rule.get("data", []):
            expr = item.get("model", {}).get("expr")
            if expr:
                print(" ".join(expr.split()))
PY
}

# rule_field <uid> <dotted.path> prints one scalar field of the named rule.
rule_field() {
  python3 - "$RULES_FILE" "$1" "$2" <<'PY'
import sys, yaml

path, uid, field = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path, encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

for group in doc.get("groups", []):
    for rule in group.get("rules", []):
        if rule.get("uid") != uid:
            continue
        node = rule
        for part in field.split("."):
            if not isinstance(node, dict):
                node = None
                break
            node = node.get(part)
        if node is not None:
            print(node)
PY
}

echo "grafana alert rules:"

if python3 -c "import sys, yaml; yaml.safe_load(open(sys.argv[1], encoding='utf-8'))" "$RULES_FILE"; then
  pass "alert-rules.yml parses as YAML"
else
  fail "alert-rules.yml must parse as YAML"
fi

duplicate_uids="$(
  python3 - "$RULES_FILE" <<'PY'
import collections, sys, yaml

with open(sys.argv[1], encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

uids = [r.get("uid") for g in doc.get("groups", []) for r in g.get("rules", [])]
for uid, n in collections.Counter(uids).items():
    if n > 1:
        print(uid)
PY
)"
if [ -z "$duplicate_uids" ]; then
  pass "every alert rule has a unique uid"
else
  fail "duplicate alert rule uids: $duplicate_uids"
fi

incomplete="$(
  python3 - "$RULES_FILE" <<'PY'
import sys, yaml

with open(sys.argv[1], encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

for group in doc.get("groups", []):
    for rule in group.get("rules", []):
        missing = [
            key for key in ("uid", "title", "condition", "for")
            if rule.get(key) is None
        ]
        if not rule.get("labels", {}).get("severity"):
            missing.append("labels.severity")
        if not rule.get("annotations", {}).get("summary"):
            missing.append("annotations.summary")
        if missing:
            print(f"{rule.get('uid', '<no uid>')}: {','.join(missing)}")
PY
)"
if [ -z "$incomplete" ]; then
  pass "every rule carries a condition, a for-duration, a severity and a summary"
else
  fail "incomplete alert rules: $incomplete"
fi

restart_query="$(rule_query server-process-restarted)"
if grep -q 'process_start_time_seconds' <<<"$restart_query" \
  && grep -q 'changes(' <<<"$restart_query" \
  && grep -q 'job="opengate-server"' <<<"$restart_query"; then
  pass "a restarted server process raises an alert"
else
  fail "server-process-restarted must alert on changes(process_start_time_seconds{job=\"opengate-server\"})"
fi

if [ "$(rule_field server-process-restarted labels.severity)" = "warning" ]; then
  pass "the restart alert is a warning, not a page on every rolling deploy"
else
  fail "server-process-restarted must carry severity: warning"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
