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

# The two container alerts below stand on a scrape that reaches each kubelet
# directly, authorised against one RBAC subresource. Get that wrong and the
# series never arrive, both alerts stay quiet, and the silence reads exactly
# like a healthy fleet. So the scrape itself is watched.
scrape_query="$(rule_query cadvisor-scrape-unreachable)"
if grep -q 'up{job="kubernetes-cadvisor"}' <<<"$scrape_query"; then
  pass "a cAdvisor scrape that is not answering raises an alert"
else
  fail "cadvisor-scrape-unreachable must alert on up{job=\"kubernetes-cadvisor\"}"
fi

# An absent series and a refused scrape are the same outcome here, so no data
# has to alert rather than resolve.
if [ "$(rule_field cadvisor-scrape-unreachable noDataState)" = "Alerting" ]; then
  pass "no data on the container scrape alerts rather than reading as healthy"
else
  fail "cadvisor-scrape-unreachable must set noDataState: Alerting — an absent series is the failure"
fi

# A pod sat at 90% of its own memory limit for three hours and nothing fired.
# The rule that was supposed to cover it read node-wide available memory, which
# says nothing about one container against one cgroup ceiling.
limit_query="$(rule_query container-memory-against-limit)"
if grep -q 'container_memory_working_set_bytes' <<<"$limit_query" \
  && grep -q 'container_spec_memory_limit_bytes' <<<"$limit_query" \
  && grep -q '/' <<<"$limit_query"; then
  pass "a container walking up to its own memory limit raises an alert"
else
  fail "container-memory-against-limit must compare working set against the container's own limit"
fi

# Node-wide memory is not a substitute: it was true and quiet throughout.
if grep -q 'node_memory_MemAvailable_bytes' <<<"$limit_query"; then
  fail "container-memory-against-limit reads node-wide memory, which is the reading that stayed quiet"
else
  pass "the container alert reads the container, not the node"
fi

if [ "$(rule_field container-memory-against-limit for)" = "10m" ]; then
  pass "the container alert waits ten minutes, so a burst does not page anyone"
else
  fail "container-memory-against-limit must carry for: 10m"
fi

# A container with no memory limit reports a limit of nought, and a comparison
# written after the division filters the quotient rather than the divisor: the
# ratio comes out +Inf, passes `> 0`, and six unlimited system containers fired
# in every message all night. The filter belongs on the limit, inside the
# division.
if grep -qF '/ (container_spec_memory_limit_bytes{container!="",container!="POD"} > 0)' <<<"$limit_query"; then
  pass "a container with no memory limit is left out of the ratio rather than read as +Inf"
else
  fail "container-memory-against-limit must divide by (container_spec_memory_limit_bytes{...} > 0), the filter on the limit"
fi

# A working set that hovers at the line fires and resolves on every evaluation:
# Loki crossed 80% twenty times in one night. The alert clears only once the
# container is back under 70%, so one crossing is one message.
thresholds="$(
  python3 - "$RULES_FILE" <<'PY'
import sys, yaml

with open(sys.argv[1], encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

for group in doc.get("groups", []):
    for rule in group.get("rules", []):
        if rule.get("uid") != "container-memory-against-limit":
            continue
        for item in rule.get("data", []):
            if item.get("refId") != rule.get("condition"):
                continue
            for cond in item.get("model", {}).get("conditions", []):
                fire = cond.get("evaluator", {})
                clear = cond.get("unloadEvaluator", {})
                print(f"{fire.get('type')} {fire.get('params')} {clear.get('type')} {clear.get('params')}")
PY
)"
if [ "$thresholds" = "gt [0.8] lt [0.7]" ]; then
  pass "the container alert fires above 80% and clears only below 70%"
else
  fail "container-memory-against-limit must fire gt 0.8 and clear with unloadEvaluator lt 0.7 (got=[$thresholds])"
fi

# And when the kill happens anyway, the alert names the container it took —
# which is the answer this incident cost six hours to reconstruct by hand.
oom_query="$(rule_query container-oom-killed)"
if grep -q 'container_oom_events_total' <<<"$oom_query" \
  && grep -q 'increase(' <<<"$oom_query"; then
  pass "a container killed for memory raises an alert"
else
  fail "container-oom-killed must alert on increase(container_oom_events_total)"
fi

if [ "$(rule_field container-oom-killed labels.severity)" = "critical" ]; then
  pass "an out-of-memory kill is critical"
else
  fail "container-oom-killed must carry severity: critical"
fi

# --- what each rule watches ---------------------------------------------------
#
# One scrape job reads the production and the staging server alike, so a rule
# over the server's series summed staging into production: seventeen alerts the
# staging server raised during a network drill read as a production rule pack
# running at five times its ceiling. Every rule says what it watches. A
# production rule reads production's namespace on every server selector it
# holds, and the shared rules — the node and the containers on it — are the ones
# a test holding the staging claim may quiet.
#
# unscoped_selectors <rules-file> prints each server selector a rule holds
# without production's namespace, each rule whose `watches` is missing or not
# one of the two, and a closing count of the server selectors it read.
unscoped_selectors() {
  python3 - "$1" <<'PY'
import re, sys, yaml

with open(sys.argv[1], encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

# A metric name with the matcher block that follows it, if any. Function names
# and keywords never reach the checks below: only a name the server publishes,
# or a matcher block naming the server's scrape job, is a server selector.
selector = re.compile(r'([A-Za-z_:][A-Za-z0-9_:]*)\s*(\{[^}]*\})?')
read = 0
for group in doc.get("groups", []):
    for rule in group.get("rules", []):
        uid = rule.get("uid", "<no uid>")
        watches = rule.get("labels", {}).get("watches")
        if watches not in ("production", "shared"):
            print(f"{uid}: watches is {watches!r}, not production or shared")
        for item in rule.get("data", []):
            expr = " ".join(str(item.get("model", {}).get("expr", "")).split())
            for name, matchers in selector.findall(expr):
                matchers = matchers or ""
                if not (name.startswith("opengate_") or 'job="opengate-server"' in matchers):
                    continue
                read += 1
                if watches != "production":
                    print(f"{uid}: reads the server's {name} but watches {watches!r}")
                if 'namespace="opengate"' not in matchers:
                    print(f"{uid}: {name}{matchers} is not scoped to production")
print(f"read={read}")
PY
}

# The defect first: a rule reading the server with no namespace, which is the
# shape every server rule had.
unscoped_demo="$(mktemp)"
cat >"$unscoped_demo" <<'DEMO'
groups:
  - rules:
      - uid: demo
        labels: { watches: production }
        data:
          - model:
              expr: sum(rate(opengate_http_requests_total[5m]))
DEMO
if grep -qF 'is not scoped to production' <<<"$(unscoped_selectors "$unscoped_demo")"; then
  pass "a server selector with no namespace is caught"
else
  fail "the scope check no longer catches a server selector with no namespace"
fi
rm -f "$unscoped_demo"

scope_report="$(unscoped_selectors "$RULES_FILE")"
scope_read="$(sed -n 's/^read=//p' <<<"$scope_report")"
scope_problems="$(grep -v '^read=' <<<"$scope_report" || true)"
if [ "${scope_read:-0}" -eq 0 ]; then
  fail "the scope check read no server selector, so it asserted an absence it never tested"
elif [ -z "$scope_problems" ]; then
  pass "every rule says what it watches, and each of $scope_read server selectors reads production alone"
else
  fail "rules reading staging as production: $scope_problems"
fi

# The rules a quiet period holds back are the shared ones, and the node and its
# containers are what those watch.
for uid in disk-usage-critical disk-usage-warning memory-usage-high \
  cadvisor-scrape-unreachable container-memory-against-limit container-oom-killed; do
  if [ "$(rule_field "$uid" labels.watches)" = "shared" ]; then
    pass "$uid watches the shared node"
  else
    fail "$uid must carry watches: shared"
  fi
done

# --- what a message carries ---------------------------------------------------
#
# A message read on a phone is the whole of what the person reading it has. The
# default one printed "Value: [no value]", every internal label Grafana adds and
# a paragraph of rationale — and not the reading, the line it crossed, where, or
# what to look at first. So every rule says, in its own units, what it saw, and
# names the first thing to check.
unexplained="$(
  python3 - "$RULES_FILE" <<'PY'
import sys, yaml

with open(sys.argv[1], encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

for group in doc.get("groups", []):
    for rule in group.get("rules", []):
        ann = rule.get("annotations", {})
        observed = ann.get("observed", "")
        # The reading is the reduced value. Guarded, because a rule firing on
        # no data has no values and an unguarded field renders as an error.
        if "with $values.B" not in observed:
            print(f"{rule['uid']}: observed must render $values.B inside a with")
        if not ann.get("check", "").strip():
            print(f"{rule['uid']}: check must name the first thing to look at")
PY
)"
if [ -z "$unexplained" ]; then
  pass "every rule says what it saw and what to check first"
else
  fail "rules a message cannot explain: $unexplained"
fi

ALERTING_DIR="$(dirname "$RULES_FILE")"
message_problems="$(
  python3 - "$ALERTING_DIR" <<'PY'
import pathlib, sys, yaml

root = pathlib.Path(sys.argv[1])
docs = {p.name: yaml.safe_load(p.read_text(encoding="utf-8")) for p in root.glob("*.yml")}

templates = [t for d in docs.values() for t in (d.get("templates") or [])]
body = "\n".join(t.get("template", "") for t in templates)
if '{{ define "opengate.telegram" }}' not in body:
    print("no template defines opengate.telegram")

# What the template must print, and the internal labels it must not.
for needle in (".Annotations.summary", ".Annotations.observed", ".Annotations.check",
               ".StartsAt", '"NoData"', '"Error"'):
    if needle not in body:
        print(f"the template does not print {needle}")
for internal in ("ref_id", "datasource_uid", "grafana_state_reason", "grafana_folder", "watches"):
    if f'"{internal}"' not in body:
        print(f"the template does not leave out {internal}")

receivers = [r for d in docs.values() for cp in (d.get("contactPoints") or [])
             for r in cp.get("receivers", [])]
for r in receivers:
    s = r.get("settings", {})
    if 'template "opengate.telegram"' not in s.get("message", ""):
        print(f"{r.get('uid')}: the message does not use opengate.telegram")
    # Summaries carry '>' and '&'; parsed as HTML they are refused by Telegram.
    if s.get("parse_mode") != "None":
        print(f"{r.get('uid')}: parse_mode must be None so the text is sent as written")
if not receivers:
    print("no contact point receivers")
PY
)"
if [ -z "$message_problems" ]; then
  pass "the Telegram message is the template that prints the reading, the place and the check"
else
  fail "the Telegram message: $message_problems"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
